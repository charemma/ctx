package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/charemma/ctx/internal/config"
	"github.com/charemma/ctx/internal/search"
	"github.com/charemma/ctx/internal/store"
	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
)

// testEmbedder returns a fixed embedding for any input.
type testEmbedder struct {
	vec []float32
}

func (e *testEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = e.vec
	}
	return out, nil
}

func (e *testEmbedder) Dimensions() int { return len(e.vec) }

// testStore is a minimal store implementation for web tests.
type testStore struct {
	store.Store
	sources    []store.Source
	syncStates map[uuid.UUID]*store.SyncState
	results    []store.SearchResult
	unembedded []store.Chunk
}

func newTestStore() *testStore {
	return &testStore{
		syncStates: make(map[uuid.UUID]*store.SyncState),
	}
}

func (s *testStore) ListSources(_ context.Context) ([]store.Source, error) {
	return s.sources, nil
}

func (s *testStore) GetSyncState(_ context.Context, id uuid.UUID) (*store.SyncState, error) {
	ss, ok := s.syncStates[id]
	if !ok {
		return nil, nil
	}
	return ss, nil
}

func (s *testStore) GetUnembeddedChunks(_ context.Context, _ int) ([]store.Chunk, error) {
	return s.unembedded, nil
}

func (s *testStore) SearchChunks(_ context.Context, _ pgvector.Vector, _ store.SearchOpts) ([]store.SearchResult, error) {
	return s.results, nil
}

func (s *testStore) GetSourceByName(_ context.Context, name string) (*store.Source, error) {
	for _, src := range s.sources {
		if src.Name == name {
			return &src, nil
		}
	}
	return nil, nil
}

func (s *testStore) CreateSource(_ context.Context, src *store.Source) error {
	s.sources = append(s.sources, *src)
	return nil
}

func (s *testStore) UpdateSyncState(_ context.Context, ss *store.SyncState) error {
	s.syncStates[ss.SourceID] = ss
	return nil
}

func newTestServer(t *testing.T) (*Server, *testStore) {
	t.Helper()
	ts := newTestStore()
	emb := &testEmbedder{vec: make([]float32, 4)}
	engine := search.New(ts, emb)
	cfg := &config.Config{
		Sources: []config.SourceConfig{
			{Name: "vault", Type: "obsidian", Location: "/tmp/vault"},
		},
	}

	srv, err := New(engine, ts, cfg)
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}
	return srv, ts
}

func TestIndexRedirectsToSearch(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("expected status %d, got %d", http.StatusFound, rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/search" {
		t.Errorf("expected redirect to /search, got %q", loc)
	}
}

func TestSearchPageWithoutQuery(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/search", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Enter a query") {
		t.Error("expected empty state message in response")
	}
}

func TestSearchPageWithResults(t *testing.T) {
	srv, ts := newTestServer(t)
	ts.results = []store.SearchResult{
		{
			ChunkID:     uuid.New(),
			Content:     "Go is great for building tools",
			HeadingPath: "Languages > Go",
			Score:       0.82,
			DocumentID:  uuid.New(),
			SourcePath:  "notes/go.md",
			Category:    "resources",
			Tags:        []string{"go"},
			LastSynced:  time.Now(),
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/search?q=golang", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "notes/go.md") {
		t.Error("expected source path in results")
	}
	if !strings.Contains(body, "0.820") {
		t.Error("expected score in results")
	}
}

func TestSearchPageNoResults(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/search?q=nonexistent", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "No results found") {
		t.Error("expected no results message")
	}
}

func TestSourcesPage(t *testing.T) {
	srv, ts := newTestServer(t)
	srcID := uuid.New()
	ts.sources = []store.Source{
		{ID: srcID, Name: "vault", Provider: "obsidian", Location: "/tmp/vault"},
	}
	now := time.Now()
	ts.syncStates[srcID] = &store.SyncState{
		SourceID:  srcID,
		LastSync:  &now,
		DocsTotal: 42,
	}

	req := httptest.NewRequest(http.MethodGet, "/sources", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "vault") {
		t.Error("expected source name in response")
	}
	if !strings.Contains(body, "obsidian") {
		t.Error("expected provider type in response")
	}
}

func TestSourcesPageEmpty(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/sources", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "No sources configured") {
		t.Error("expected empty state message")
	}
}

func TestSyncRedirects(t *testing.T) {
	srv, _ := newTestServer(t)

	form := url.Values{"source": {"vault"}}
	req := httptest.NewRequest(http.MethodPost, "/sources/sync", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	// Sync will fail (no real vault), but should redirect back to sources.
	if rec.Code != http.StatusSeeOther {
		t.Errorf("expected redirect status 303, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/sources") {
		t.Errorf("expected redirect to /sources, got %q", loc)
	}
}

func TestSyncNoSourceRedirects(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/sources/sync", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("expected redirect, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "error=no") {
		t.Errorf("expected error in redirect, got %q", loc)
	}
}

func TestAPISearchReturnsJSON(t *testing.T) {
	srv, ts := newTestServer(t)
	ts.results = []store.SearchResult{
		{
			ChunkID:    uuid.New(),
			Content:    "test content",
			Score:      0.75,
			DocumentID: uuid.New(),
			SourcePath: "test.md",
			LastSynced: time.Now(),
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/search?q=test", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}

	var resp apiSearchResult
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding JSON: %v", err)
	}
	if resp.Query != "test" {
		t.Errorf("expected query 'test', got %q", resp.Query)
	}
	if resp.Count != 1 {
		t.Errorf("expected count 1, got %d", resp.Count)
	}
	if resp.Results[0].Score != 0.75 {
		t.Errorf("expected score 0.75, got %f", resp.Results[0].Score)
	}
}

func TestAPISearchMissingQuery(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/search", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestAPISearchWithFilters(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/search?q=test&category=resources&limit=5", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestStaticFilesServed(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/static/style.css", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/css") {
		t.Errorf("expected text/css content type, got %q", ct)
	}
}
