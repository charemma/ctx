package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/charemma/ctx/internal/config"
	"github.com/charemma/ctx/internal/provider/obsidian"
	"github.com/charemma/ctx/internal/search"
	"github.com/charemma/ctx/internal/store"
	"github.com/google/uuid"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Server is the web UI server for ctx.
type Server struct {
	engine *search.Engine
	store  store.Store
	cfg    *config.Config
	pages  map[string]*template.Template
	mux    *http.ServeMux
}

// New creates a web server with the given dependencies.
func New(engine *search.Engine, st store.Store, cfg *config.Config) (*Server, error) {
	funcMap := template.FuncMap{
		"scoreClass": scoreClass,
		"snippet":    snippet,
	}

	pages := map[string]*template.Template{}
	for _, name := range []string{"search.html", "sources.html"} {
		t, err := template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/layout.html", "templates/"+name)
		if err != nil {
			return nil, fmt.Errorf("parsing template %s: %w", name, err)
		}
		pages[name] = t
	}

	s := &Server{
		engine: engine,
		store:  st,
		cfg:    cfg,
		pages:  pages,
		mux:    http.NewServeMux(),
	}

	s.routes()
	return s, nil
}

func (s *Server) routes() {
	staticSub, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	s.mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux.HandleFunc("GET /search", s.handleSearch)
	s.mux.HandleFunc("GET /sources", s.handleSources)
	s.mux.HandleFunc("POST /sources/sync", s.handleSync)
	s.mux.HandleFunc("GET /api/search", s.handleAPISearch)
}

// Handler returns the HTTP handler for the server.
func (s *Server) Handler() http.Handler { return s.mux }

// ListenAndServe starts the server on the given address.
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.ListenAndServe()
}

// page holds common template data.
type page struct {
	Title     string
	ActiveNav string
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/search", http.StatusFound)
}

// searchPage holds data for the search template.
type searchPage struct {
	page
	Query    string
	Category string
	Tags     string
	Results  []search.Result
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	category := r.URL.Query().Get("category")
	tags := r.URL.Query().Get("tags")

	data := searchPage{
		page:     page{Title: "Search", ActiveNav: "search"},
		Query:    q,
		Category: category,
		Tags:     tags,
	}

	if q != "" {
		query := search.Query{
			Text:     q,
			Category: category,
			Limit:    20,
		}
		if tags != "" {
			query.Tags = strings.Split(tags, ",")
			for i := range query.Tags {
				query.Tags[i] = strings.TrimSpace(query.Tags[i])
			}
		}

		results, err := s.engine.Search(r.Context(), query)
		if err != nil {
			slog.Error("search failed", "error", err)
		} else {
			data.Results = results
		}
	}

	s.render(w, "search.html", data)
}

// sourceRow holds data for a single source in the sources template.
type sourceRow struct {
	Name        string
	Provider    string
	Location    string
	LastSync    *time.Time
	DocsTotal   int
	ChunksTotal int
}

// sourcesPage holds data for the sources template.
type sourcesPage struct {
	page
	Sources     []sourceRow
	TotalDocs   int64
	TotalChunks int64
	Embedded    int
	Pending     int
	Flash       string
	FlashError  string
}

func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	data := sourcesPage{
		page: page{Title: "Sources", ActiveNav: "sources"},
	}
	data.Flash = r.URL.Query().Get("flash")
	data.FlashError = r.URL.Query().Get("error")

	sources, err := s.store.ListSources(ctx)
	if err != nil {
		slog.Error("listing sources", "error", err)
	}

	for _, src := range sources {
		row := sourceRow{
			Name:     src.Name,
			Provider: src.Provider,
			Location: src.Location,
		}
		ss, err := s.store.GetSyncState(ctx, src.ID)
		if err == nil && ss != nil {
			row.LastSync = ss.LastSync
			row.DocsTotal = ss.DocsTotal
		}
		data.Sources = append(data.Sources, row)
	}

	counts, err := s.tableCounts(ctx)
	if err == nil {
		data.TotalDocs = counts["documents"]
		data.TotalChunks = counts["chunks"]
	}

	unembedded, err := s.store.GetUnembeddedChunks(ctx, 0)
	if err == nil {
		data.Pending = len(unembedded)
		data.Embedded = int(data.TotalChunks) - data.Pending
		if data.Embedded < 0 {
			data.Embedded = 0
		}
	}

	s.render(w, "sources.html", data)
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/sources?error=invalid+form", http.StatusSeeOther)
		return
	}
	sourceName := r.FormValue("source")
	if sourceName == "" {
		http.Redirect(w, r, "/sources?error=no+source+specified", http.StatusSeeOther)
		return
	}

	// Find the source config.
	var srcCfg *config.SourceConfig
	for _, sc := range s.cfg.Sources {
		if sc.Name == sourceName {
			srcCfg = &sc
			break
		}
	}
	if srcCfg == nil {
		http.Redirect(w, r, "/sources?error=source+not+found", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	if err := syncSourceByConfig(ctx, s.store, *srcCfg); err != nil {
		slog.Error("sync failed", "source", sourceName, "error", err)
		http.Redirect(w, r, "/sources?error=sync+failed:+"+err.Error(), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/sources?flash=Sync+completed+for+"+sourceName, http.StatusSeeOther)
}

// apiSearchResult is the JSON response for the search API.
type apiSearchResult struct {
	Query   string          `json:"query"`
	Count   int             `json:"count"`
	Results []apiResultItem `json:"results"`
}

type apiResultItem struct {
	Score       float64  `json:"score"`
	Content     string   `json:"content"`
	SourcePath  string   `json:"source_path"`
	HeadingPath string   `json:"heading_path,omitempty"`
	Category    string   `json:"category,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	LastSynced  string   `json:"last_synced"`
}

func (s *Server) handleAPISearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query parameter 'q' is required"})
		return
	}

	category := r.URL.Query().Get("category")
	limitStr := r.URL.Query().Get("limit")
	limit := 10
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}

	query := search.Query{
		Text:     q,
		Category: category,
		Limit:    limit,
	}

	results, err := s.engine.Search(r.Context(), query)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	items := make([]apiResultItem, len(results))
	for i, r := range results {
		items[i] = apiResultItem{
			Score:       r.Score,
			Content:     r.Content,
			SourcePath:  r.SourcePath,
			HeadingPath: r.HeadingPath,
			Category:    r.Category,
			Tags:        r.Tags,
			LastSynced:  r.LastSynced.Format(time.RFC3339),
		}
	}

	writeJSON(w, http.StatusOK, apiSearchResult{
		Query:   q,
		Count:   len(items),
		Results: items,
	})
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	t, ok := s.pages[name]
	if !ok {
		slog.Error("template not found", "template", name)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout.html", data); err != nil {
		slog.Error("rendering template", "template", name, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writing JSON response", "error", err)
	}
}

// tableCounts attempts to get table counts via a type assertion to PostgresStore.
func (s *Server) tableCounts(ctx context.Context) (map[string]int64, error) {
	type tableCounter interface {
		TableCounts(ctx context.Context) (map[string]int64, error)
	}
	if tc, ok := s.store.(tableCounter); ok {
		return tc.TableCounts(ctx)
	}
	return map[string]int64{}, nil
}

// syncSourceByConfig runs a sync for a single source config.
func syncSourceByConfig(ctx context.Context, st store.Store, srcCfg config.SourceConfig) error {
	// Ensure source record exists.
	existing, err := st.GetSourceByName(ctx, srcCfg.Name)
	if err != nil {
		return fmt.Errorf("checking source: %w", err)
	}
	if existing == nil {
		src := &store.Source{
			Provider: srcCfg.Type,
			Name:     srcCfg.Name,
			Location: srcCfg.Location,
		}
		if err := st.CreateSource(ctx, src); err != nil {
			return fmt.Errorf("creating source: %w", err)
		}
	}

	var sourceID uuid.UUID
	src, err := st.GetSourceByName(ctx, srcCfg.Name)
	if err != nil || src == nil {
		return fmt.Errorf("getting source ID for %q: %w", srcCfg.Name, err)
	}
	sourceID = src.ID

	switch srcCfg.Type {
	case "obsidian":
		op := obsidian.New(srcCfg.Name, srcCfg.Location, nil)
		stats, err := op.Sync(ctx, st)
		if err != nil {
			return fmt.Errorf("sync failed: %w", err)
		}
		now := time.Now()
		syncState := &store.SyncState{
			SourceID:    sourceID,
			LastSync:    &now,
			DocsTotal:   stats.FilesFound,
			DocsSynced:  stats.FilesNew + stats.FilesUpdated,
			DocsSkipped: stats.FilesSkipped,
		}
		return st.UpdateSyncState(ctx, syncState)
	default:
		return fmt.Errorf("unsupported source type: %q", srcCfg.Type)
	}
}

// Template helpers

func scoreClass(score float64) string {
	switch {
	case score >= 0.7:
		return "score-high"
	case score >= 0.5:
		return "score-mid"
	default:
		return "score-low"
	}
}

func snippet(content string) string {
	s := strings.ReplaceAll(content, "\n", " ")
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}
