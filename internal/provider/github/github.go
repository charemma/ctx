package github

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/charemma/ctx/internal/provider"
	"github.com/charemma/ctx/internal/store"
	"github.com/google/uuid"
)

// GHConfig holds GitHub-specific source configuration.
type GHConfig struct {
	IncludePRs      bool   `json:"include_prs"`
	IncludeComments bool   `json:"include_comments"`
	State           string `json:"state"` // open, closed, all
}

// Issue represents a GitHub issue or pull request from the API.
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	Labels    []Label   `json:"labels"`
	Assignees []User    `json:"assignees"`
	User      User      `json:"user"`
	Milestone *MS       `json:"milestone"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	PullReq   *PRRef    `json:"pull_request"`
}

// IsPR returns true if the issue is a pull request.
func (i Issue) IsPR() bool {
	return i.PullReq != nil
}

// Label represents a GitHub label.
type Label struct {
	Name string `json:"name"`
}

// User represents a GitHub user.
type User struct {
	Login string `json:"login"`
}

// MS represents a GitHub milestone.
type MS struct {
	Title string `json:"title"`
}

// PRRef is the pull_request field on an issue (non-nil means it is a PR).
type PRRef struct {
	URL string `json:"url"`
}

// Comment represents a GitHub issue/PR comment.
type Comment struct {
	ID        int       `json:"id"`
	Body      string    `json:"body"`
	User      User      `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CommandRunner abstracts command execution for testing.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// execRunner is the default runner that shells out to the real command.
func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Output()
}

// Provider syncs GitHub issues and pull requests into the knowledge store.
type Provider struct {
	name     string
	repo     string // owner/repo
	cfg      GHConfig
	sourceID uuid.UUID
	runner   CommandRunner
}

// New creates a GitHub provider for the given repository.
func New(name, repo string, cfg GHConfig) *Provider {
	if cfg.State == "" {
		cfg.State = "all"
	}
	return &Provider{
		name:   name,
		repo:   repo,
		cfg:    cfg,
		runner: execRunner,
	}
}

func (p *Provider) Name() string { return p.name }
func (p *Provider) Type() string { return "github" }

// Sync fetches issues (and optionally PRs) from GitHub and upserts them.
func (p *Provider) Sync(ctx context.Context, st store.Store) (*provider.SyncStats, error) {
	stats := &provider.SyncStats{}

	src, err := st.GetSourceByName(ctx, p.name)
	if err != nil {
		return nil, fmt.Errorf("getting source %q: %w", p.name, err)
	}
	p.sourceID = src.ID

	// Determine the "since" cursor for incremental sync.
	var since *time.Time
	syncState, err := st.GetSyncState(ctx, p.sourceID)
	if err == nil && syncState != nil && syncState.Cursor != nil {
		var cursor SyncCursor
		if json.Unmarshal(syncState.Cursor, &cursor) == nil && !cursor.LastUpdated.IsZero() {
			since = &cursor.LastUpdated
		}
	}

	issues, err := p.fetchIssues(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("fetching issues: %w", err)
	}

	// Track the latest updated_at across all fetched items for the cursor.
	var latestUpdated time.Time

	// Get existing document paths for deletion detection on full syncs.
	existingPaths, err := st.ListDocumentPaths(ctx, p.sourceID)
	if err != nil {
		return nil, fmt.Errorf("listing existing paths: %w", err)
	}
	diskSet := make(map[string]struct{})

	for _, issue := range issues {
		if issue.IsPR() && !p.cfg.IncludePRs {
			continue
		}

		stats.FilesFound++

		if issue.UpdatedAt.After(latestUpdated) {
			latestUpdated = issue.UpdatedAt
		}

		path := issuePath(issue)
		diskSet[path] = struct{}{}

		// Build content and metadata.
		hash := contentHash(issue)

		existing, err := st.GetDocumentByPath(ctx, p.sourceID, path)
		if err == nil && existing != nil && existing.ContentHash == hash {
			stats.FilesSkipped++
			continue
		}

		tags := extractLabels(issue)
		metadata := buildMetadata(issue)
		metaJSON, _ := json.Marshal(metadata)

		title := issueTitle(issue)
		category := "projects"

		doc := &store.Document{
			SourceID:     p.sourceID,
			SourcePath:   path,
			Title:        strPtr(title),
			ContentHash:  hash,
			ContentType:  "text/markdown",
			ParaCategory: strPtr(category),
			Tags:         tags,
			Metadata:     metaJSON,
		}

		if existing != nil {
			doc.ID = existing.ID
			stats.FilesUpdated++
		} else {
			doc.ID = uuid.New()
			stats.FilesNew++
		}

		if err := st.UpsertDocument(ctx, doc); err != nil {
			return nil, fmt.Errorf("upserting document %s: %w", path, err)
		}

		// Delete old chunks, create new ones.
		if err := st.DeleteChunksByDocument(ctx, doc.ID); err != nil {
			return nil, fmt.Errorf("deleting chunks for %s: %w", path, err)
		}

		chunks := p.chunkIssue(ctx, doc.ID, issue)
		if len(chunks) > 0 {
			if err := st.CreateChunks(ctx, chunks); err != nil {
				return nil, fmt.Errorf("creating chunks for %s: %w", path, err)
			}
			stats.ChunksCreated += len(chunks)
		}
	}

	// Detect deletions only on full syncs (no cursor).
	if since == nil {
		for _, ep := range existingPaths {
			if _, ok := diskSet[ep]; !ok {
				stats.FilesDeleted++
			}
		}
		if stats.FilesDeleted > 0 {
			var deletedPaths []string
			for _, ep := range existingPaths {
				if _, ok := diskSet[ep]; !ok {
					deletedPaths = append(deletedPaths, ep)
				}
			}
			if err := st.DeleteDocumentsByPaths(ctx, p.sourceID, deletedPaths); err != nil {
				return nil, fmt.Errorf("deleting removed documents: %w", err)
			}
		}
	}

	// Update cursor.
	if !latestUpdated.IsZero() {
		cursor := SyncCursor{LastUpdated: latestUpdated}
		cursorJSON, _ := json.Marshal(cursor)
		now := time.Now()
		syncState := &store.SyncState{
			SourceID:    p.sourceID,
			LastSync:    &now,
			DocsTotal:   stats.FilesFound,
			DocsSynced:  stats.FilesNew + stats.FilesUpdated,
			DocsSkipped: stats.FilesSkipped,
			Cursor:      cursorJSON,
		}
		if err := st.UpdateSyncState(ctx, syncState); err != nil {
			return nil, fmt.Errorf("updating sync state: %w", err)
		}
	}

	return stats, nil
}

// SyncCursor tracks the last updated timestamp for incremental sync.
type SyncCursor struct {
	LastUpdated time.Time `json:"last_updated"`
}

// fetchIssues retrieves issues from GitHub via gh api, paginating as needed.
// If since is set, only issues updated after that time are returned.
func (p *Provider) fetchIssues(ctx context.Context, since *time.Time) ([]Issue, error) {
	var all []Issue

	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("/repos/%s/issues?state=%s&per_page=100&page=%d&sort=updated&direction=desc",
			p.repo, p.cfg.State, page)
		if since != nil {
			endpoint += "&since=" + since.Format(time.RFC3339)
		}

		out, err := p.runner(ctx, "gh", "api", endpoint)
		if err != nil {
			return nil, fmt.Errorf("gh api page %d: %w", page, err)
		}

		var issues []Issue
		if err := json.Unmarshal(out, &issues); err != nil {
			return nil, fmt.Errorf("parsing issues page %d: %w", page, err)
		}

		if len(issues) == 0 {
			break
		}

		all = append(all, issues...)

		if len(issues) < 100 {
			break
		}
	}

	return all, nil
}

// fetchComments retrieves comments for a single issue/PR.
func (p *Provider) fetchComments(ctx context.Context, number int) ([]Comment, error) {
	var all []Comment

	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100&page=%d",
			p.repo, number, page)

		out, err := p.runner(ctx, "gh", "api", endpoint)
		if err != nil {
			return nil, fmt.Errorf("gh api comments page %d: %w", page, err)
		}

		var comments []Comment
		if err := json.Unmarshal(out, &comments); err != nil {
			return nil, fmt.Errorf("parsing comments page %d: %w", page, err)
		}

		if len(comments) == 0 {
			break
		}

		all = append(all, comments...)

		if len(comments) < 100 {
			break
		}
	}

	return all, nil
}

// chunkIssue creates chunks for an issue body and its comments.
func (p *Provider) chunkIssue(ctx context.Context, docID uuid.UUID, issue Issue) []store.Chunk {
	var chunks []store.Chunk
	idx := 0

	// Body chunk.
	body := strings.TrimSpace(issue.Body)
	if body != "" {
		kind := "issue"
		if issue.IsPR() {
			kind = "pr"
		}
		heading := fmt.Sprintf("#%d %s", issue.Number, issue.Title)
		content := fmt.Sprintf("[%s] %s\n\n%s", kind, heading, body)

		chunks = append(chunks, store.Chunk{
			ID:          uuid.New(),
			DocumentID:  docID,
			ChunkIndex:  idx,
			Content:     content,
			HeadingPath: strPtr(heading),
			TokenCount:  intPtr(estimateTokens(content)),
		})
		idx++
	}

	// Comment chunks.
	if p.cfg.IncludeComments {
		comments, err := p.fetchComments(ctx, issue.Number)
		if err == nil {
			for _, c := range comments {
				commentBody := strings.TrimSpace(c.Body)
				if commentBody == "" {
					continue
				}
				heading := fmt.Sprintf("#%d comment by %s", issue.Number, c.User.Login)
				content := fmt.Sprintf("[comment] %s\n\n%s", heading, commentBody)

				chunks = append(chunks, store.Chunk{
					ID:          uuid.New(),
					DocumentID:  docID,
					ChunkIndex:  idx,
					Content:     content,
					HeadingPath: strPtr(heading),
					TokenCount:  intPtr(estimateTokens(content)),
				})
				idx++
			}
		}
	}

	return chunks
}

func issuePath(issue Issue) string {
	kind := "issues"
	if issue.IsPR() {
		kind = "pulls"
	}
	return fmt.Sprintf("%s/%d", kind, issue.Number)
}

func issueTitle(issue Issue) string {
	prefix := "Issue"
	if issue.IsPR() {
		prefix = "PR"
	}
	return fmt.Sprintf("%s #%d: %s", prefix, issue.Number, issue.Title)
}

func extractLabels(issue Issue) []string {
	if len(issue.Labels) == 0 {
		return nil
	}
	tags := make([]string, len(issue.Labels))
	for i, l := range issue.Labels {
		tags[i] = l.Name
	}
	return tags
}

func buildMetadata(issue Issue) map[string]any {
	m := map[string]any{
		"number":     issue.Number,
		"state":      issue.State,
		"author":     issue.User.Login,
		"created_at": issue.CreatedAt.Format(time.RFC3339),
		"updated_at": issue.UpdatedAt.Format(time.RFC3339),
	}

	if issue.IsPR() {
		m["type"] = "pull_request"
	} else {
		m["type"] = "issue"
	}

	if len(issue.Assignees) > 0 {
		assignees := make([]string, len(issue.Assignees))
		for i, a := range issue.Assignees {
			assignees[i] = a.Login
		}
		m["assignees"] = assignees
	}

	if issue.Milestone != nil {
		m["milestone"] = issue.Milestone.Title
	}

	return m
}

func contentHash(issue Issue) string {
	// Hash the body + updated_at to detect changes including comment updates.
	data := issue.Body + "|" + issue.UpdatedAt.Format(time.RFC3339Nano) + "|" + strconv.Itoa(issue.Number)
	h := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", h)
}

// estimateTokens provides a rough token count (1 token ~ 4 chars).
func estimateTokens(s string) int {
	return len(s) / 4
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func intPtr(n int) *int {
	return &n
}
