package obsidian

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParaCategory(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"1 Projects/my-project/notes.md", "projects"},
		{"2 Areas/Health/tracking.md", "areas"},
		{"3 Resources/AI/Patterns.md", "resources"},
		{"4 Archive/old-project/readme.md", "archive"},
		{"Journal/2026/04-April/2026-04-18-Saturday.md", "journal"},
		{"Inbox.md", "inbox"},
		{"random-note.md", "other"},
		{"subfolder/deep/note.md", "other"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := paraCategory(tt.path)
			if got != tt.expected {
				t.Errorf("paraCategory(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

func TestExcludePatternMatching(t *testing.T) {
	p := &Provider{
		excludes: []string{"*.excl", "draft-*"},
	}

	tests := []struct {
		path     string
		excluded bool
	}{
		{"notes.md", false},
		{"notes.excl", true},
		{"draft-wip.md", true},
		{"folder/draft-wip.md", true},
		{"folder/notes.md", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := p.isExcluded(tt.path)
			if got != tt.excluded {
				t.Errorf("isExcluded(%q) = %v, want %v", tt.path, got, tt.excluded)
			}
		})
	}
}

func TestWalkVault_SkipsDefaultDirs(t *testing.T) {
	root := t.TempDir()

	// Create directories that should be skipped.
	for _, dir := range []string{".obsidian", ".trash", ".stfolder", "Templates"} {
		os.MkdirAll(filepath.Join(root, dir), 0o755)
		os.WriteFile(filepath.Join(root, dir, "skip.md"), []byte("skip"), 0o644)
	}

	// Create files that should be found.
	os.WriteFile(filepath.Join(root, "note.md"), []byte("hello"), 0o644)
	os.MkdirAll(filepath.Join(root, "1 Projects"), 0o755)
	os.WriteFile(filepath.Join(root, "1 Projects", "task.md"), []byte("task"), 0o644)

	// Non-markdown files should be skipped.
	os.WriteFile(filepath.Join(root, "image.png"), []byte("png"), 0o644)

	p := &Provider{vaultDir: root}
	paths, err := p.walkVault()
	if err != nil {
		t.Fatalf("walkVault error: %v", err)
	}

	expected := map[string]bool{
		"note.md":                true,
		filepath.Join("1 Projects", "task.md"): true,
	}

	if len(paths) != len(expected) {
		t.Fatalf("expected %d paths, got %d: %v", len(expected), len(paths), paths)
	}

	for _, p := range paths {
		if !expected[p] {
			t.Errorf("unexpected path: %q", p)
		}
	}
}

func TestExtractTitle(t *testing.T) {
	tests := []struct {
		name     string
		parsed   ParseResult
		relPath  string
		expected string
	}{
		{
			name: "from frontmatter",
			parsed: ParseResult{
				Frontmatter: Frontmatter{Extra: map[string]string{"title": "My Title"}},
				Body:        "content",
			},
			relPath:  "note.md",
			expected: "My Title",
		},
		{
			name: "from heading",
			parsed: ParseResult{
				Frontmatter: Frontmatter{Extra: map[string]string{}},
				Body:        "# Document Title\n\nContent here.",
			},
			relPath:  "note.md",
			expected: "Document Title",
		},
		{
			name: "from filename",
			parsed: ParseResult{
				Frontmatter: Frontmatter{Extra: map[string]string{}},
				Body:        "No heading here.",
			},
			relPath:  "3 Resources/my-note.md",
			expected: "my-note",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTitle(tt.parsed, tt.relPath)
			if got != tt.expected {
				t.Errorf("extractTitle() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExtractWikiLinks(t *testing.T) {
	body := "See [[Some Note]] and also [[Another|display text]]. Link to [[Some Note]] again."

	links := extractWikiLinks(body)

	if len(links) != 2 {
		t.Fatalf("expected 2 unique links, got %d: %v", len(links), links)
	}
	if links[0] != "Some Note" {
		t.Errorf("expected 'Some Note', got %q", links[0])
	}
	if links[1] != "Another" {
		t.Errorf("expected 'Another', got %q", links[1])
	}
}

func TestContentHash(t *testing.T) {
	h1 := contentHash([]byte("hello"))
	h2 := contentHash([]byte("hello"))
	h3 := contentHash([]byte("world"))

	if h1 != h2 {
		t.Error("same content should produce same hash")
	}
	if h1 == h3 {
		t.Error("different content should produce different hash")
	}
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex hash, got length %d", len(h1))
	}
}
