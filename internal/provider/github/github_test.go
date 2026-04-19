package github

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
)

func testdataPath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", name)
}

func loadTestIssues(t *testing.T) []Issue {
	t.Helper()
	data, err := os.ReadFile(testdataPath("issues.json"))
	if err != nil {
		t.Fatalf("reading test fixture: %v", err)
	}
	var issues []Issue
	if err := json.Unmarshal(data, &issues); err != nil {
		t.Fatalf("parsing test fixture: %v", err)
	}
	return issues
}

func loadTestComments(t *testing.T) []Comment {
	t.Helper()
	data, err := os.ReadFile(testdataPath("comments.json"))
	if err != nil {
		t.Fatalf("reading test fixture: %v", err)
	}
	var comments []Comment
	if err := json.Unmarshal(data, &comments); err != nil {
		t.Fatalf("parsing test fixture: %v", err)
	}
	return comments
}

func TestIssueParsing(t *testing.T) {
	issues := loadTestIssues(t)

	if len(issues) != 3 {
		t.Fatalf("expected 3 issues, got %d", len(issues))
	}

	t.Run("issue fields", func(t *testing.T) {
		issue := issues[0]
		if issue.Number != 1 {
			t.Errorf("expected number 1, got %d", issue.Number)
		}
		if issue.Title != "Add search functionality" {
			t.Errorf("unexpected title: %q", issue.Title)
		}
		if issue.State != "open" {
			t.Errorf("expected state open, got %q", issue.State)
		}
		if issue.User.Login != "babis" {
			t.Errorf("expected author babis, got %q", issue.User.Login)
		}
		if issue.Milestone == nil || issue.Milestone.Title != "v1.0" {
			t.Error("expected milestone v1.0")
		}
	})

	t.Run("pr detection", func(t *testing.T) {
		if issues[0].IsPR() {
			t.Error("issue 1 should not be a PR")
		}
		if !issues[2].IsPR() {
			t.Error("issue 3 should be a PR")
		}
	})

	t.Run("labels", func(t *testing.T) {
		if len(issues[0].Labels) != 2 {
			t.Fatalf("expected 2 labels, got %d", len(issues[0].Labels))
		}
		if issues[0].Labels[0].Name != "enhancement" {
			t.Errorf("expected label 'enhancement', got %q", issues[0].Labels[0].Name)
		}
	})
}

func TestExtractLabels(t *testing.T) {
	tests := []struct {
		name     string
		issue    Issue
		expected []string
	}{
		{
			name:     "no labels",
			issue:    Issue{},
			expected: nil,
		},
		{
			name: "multiple labels",
			issue: Issue{Labels: []Label{
				{Name: "bug"},
				{Name: "critical"},
			}},
			expected: []string{"bug", "critical"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractLabels(tt.issue)
			if tt.expected == nil && got != nil {
				t.Errorf("expected nil, got %v", got)
				return
			}
			if len(got) != len(tt.expected) {
				t.Errorf("expected %d labels, got %d", len(tt.expected), len(got))
				return
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("label %d: expected %q, got %q", i, tt.expected[i], got[i])
				}
			}
		})
	}
}

func TestBuildMetadata(t *testing.T) {
	issues := loadTestIssues(t)

	t.Run("issue metadata", func(t *testing.T) {
		m := buildMetadata(issues[0])
		if m["type"] != "issue" {
			t.Errorf("expected type 'issue', got %v", m["type"])
		}
		if m["state"] != "open" {
			t.Errorf("expected state 'open', got %v", m["state"])
		}
		if m["author"] != "babis" {
			t.Errorf("expected author 'babis', got %v", m["author"])
		}
		if m["milestone"] != "v1.0" {
			t.Errorf("expected milestone 'v1.0', got %v", m["milestone"])
		}
		assignees, ok := m["assignees"].([]string)
		if !ok || len(assignees) != 1 || assignees[0] != "babis" {
			t.Errorf("unexpected assignees: %v", m["assignees"])
		}
	})

	t.Run("pr metadata", func(t *testing.T) {
		m := buildMetadata(issues[2])
		if m["type"] != "pull_request" {
			t.Errorf("expected type 'pull_request', got %v", m["type"])
		}
	})

	t.Run("no milestone", func(t *testing.T) {
		m := buildMetadata(issues[1])
		if _, ok := m["milestone"]; ok {
			t.Error("expected no milestone key")
		}
	})

	t.Run("no assignees", func(t *testing.T) {
		m := buildMetadata(issues[1])
		if _, ok := m["assignees"]; ok {
			t.Error("expected no assignees key")
		}
	})
}

func TestIssuePath(t *testing.T) {
	tests := []struct {
		name     string
		issue    Issue
		expected string
	}{
		{
			name:     "issue path",
			issue:    Issue{Number: 42},
			expected: "issues/42",
		},
		{
			name:     "pr path",
			issue:    Issue{Number: 10, PullReq: &PRRef{URL: "https://example.com"}},
			expected: "pulls/10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := issuePath(tt.issue)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestIssueTitle(t *testing.T) {
	tests := []struct {
		name     string
		issue    Issue
		expected string
	}{
		{
			name:     "issue title",
			issue:    Issue{Number: 1, Title: "Bug fix"},
			expected: "Issue #1: Bug fix",
		},
		{
			name:     "pr title",
			issue:    Issue{Number: 5, Title: "Refactor", PullReq: &PRRef{}},
			expected: "PR #5: Refactor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := issueTitle(tt.issue)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestChunkIssue_BodyOnly(t *testing.T) {
	p := &Provider{
		name: "test",
		repo: "owner/repo",
		cfg:  GHConfig{IncludeComments: false},
	}

	issue := Issue{
		Number: 1,
		Title:  "Test issue",
		Body:   "This is the body.",
		State:  "open",
	}

	chunks := p.chunkIssue(context.Background(), uuid.New(), issue)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}

	chunk := chunks[0]
	if chunk.ChunkIndex != 0 {
		t.Errorf("expected chunk index 0, got %d", chunk.ChunkIndex)
	}
	if chunk.HeadingPath == nil || *chunk.HeadingPath != "#1 Test issue" {
		t.Errorf("unexpected heading: %v", chunk.HeadingPath)
	}
	if chunk.Content != "[issue] #1 Test issue\n\nThis is the body." {
		t.Errorf("unexpected content: %q", chunk.Content)
	}
}

func TestChunkIssue_PRBody(t *testing.T) {
	p := &Provider{
		name: "test",
		repo: "owner/repo",
		cfg:  GHConfig{IncludeComments: false},
	}

	issue := Issue{
		Number:  3,
		Title:   "Refactor store",
		Body:    "This PR refactors the store.",
		PullReq: &PRRef{URL: "https://example.com"},
	}

	chunks := p.chunkIssue(context.Background(), uuid.New(), issue)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}

	if chunks[0].Content != "[pr] #3 Refactor store\n\nThis PR refactors the store." {
		t.Errorf("unexpected PR chunk content: %q", chunks[0].Content)
	}
}

func TestChunkIssue_EmptyBody(t *testing.T) {
	p := &Provider{
		name: "test",
		repo: "owner/repo",
		cfg:  GHConfig{IncludeComments: false},
	}

	issue := Issue{
		Number: 5,
		Title:  "No body",
		Body:   "",
	}

	chunks := p.chunkIssue(context.Background(), uuid.New(), issue)
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty body, got %d", len(chunks))
	}
}

func TestChunkIssue_WithComments(t *testing.T) {
	commentsJSON, err := os.ReadFile(testdataPath("comments.json"))
	if err != nil {
		t.Fatalf("reading comments fixture: %v", err)
	}

	// Mock runner returns comments for any issue comment request.
	mockRunner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		// First call returns comments, second call (next page) returns empty.
		for _, arg := range args {
			if arg == "/repos/owner/repo/issues/1/comments?per_page=100&page=1" {
				return commentsJSON, nil
			}
		}
		return []byte("[]"), nil
	}

	p := &Provider{
		name:   "test",
		repo:   "owner/repo",
		cfg:    GHConfig{IncludeComments: true},
		runner: mockRunner,
	}

	issue := Issue{
		Number: 1,
		Title:  "Test issue",
		Body:   "Issue body here.",
	}

	chunks := p.chunkIssue(context.Background(), uuid.New(), issue)

	// 1 body chunk + 2 comment chunks.
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	// Verify comment chunks.
	if chunks[1].ChunkIndex != 1 {
		t.Errorf("expected chunk index 1, got %d", chunks[1].ChunkIndex)
	}
	if chunks[1].HeadingPath == nil || *chunks[1].HeadingPath != "#1 comment by reviewer" {
		t.Errorf("unexpected comment heading: %v", chunks[1].HeadingPath)
	}

	if chunks[2].HeadingPath == nil || *chunks[2].HeadingPath != "#1 comment by babis" {
		t.Errorf("unexpected comment heading: %v", chunks[2].HeadingPath)
	}
}

func TestContentHash(t *testing.T) {
	ts := time.Date(2026, 4, 15, 14, 30, 0, 0, time.UTC)

	issue1 := Issue{Number: 1, Body: "body", UpdatedAt: ts}
	issue2 := Issue{Number: 1, Body: "body", UpdatedAt: ts}
	issue3 := Issue{Number: 1, Body: "changed", UpdatedAt: ts}

	h1 := contentHash(issue1)
	h2 := contentHash(issue2)
	h3 := contentHash(issue3)

	if h1 != h2 {
		t.Error("same issue should produce same hash")
	}
	if h1 == h3 {
		t.Error("different body should produce different hash")
	}
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex hash, got length %d", len(h1))
	}
}

func TestChangeDetection_SinceCursor(t *testing.T) {
	ts := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	cursor := SyncCursor{LastUpdated: ts}
	data, err := json.Marshal(cursor)
	if err != nil {
		t.Fatalf("marshaling cursor: %v", err)
	}

	var parsed SyncCursor
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshaling cursor: %v", err)
	}

	if parsed.LastUpdated.IsZero() {
		t.Error("parsed cursor should have non-zero timestamp")
	}
	if !parsed.LastUpdated.Equal(ts) {
		t.Errorf("expected %v, got %v", ts, parsed.LastUpdated)
	}
}

func TestGHConfig_Defaults(t *testing.T) {
	p := New("test", "owner/repo", GHConfig{})

	if p.cfg.State != "all" {
		t.Errorf("expected default state 'all', got %q", p.cfg.State)
	}
	if p.cfg.IncludePRs {
		t.Error("expected IncludePRs to default to false")
	}
	if p.cfg.IncludeComments {
		t.Error("expected IncludeComments to default to false")
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"abcd", 1},
		{"hello world this is a test", 6},
	}

	for _, tt := range tests {
		got := estimateTokens(tt.input)
		if got != tt.expected {
			t.Errorf("estimateTokens(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}
