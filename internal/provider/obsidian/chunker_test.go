package obsidian

import (
	"strings"
	"testing"
)

func TestChunkMarkdown_ByHeadings(t *testing.T) {
	body := "Intro paragraph.\n\n## Section One\n\nContent of section one.\n\n## Section Two\n\nContent of section two."
	meta := ChunkMeta{
		SourcePath: "3 Resources/AI/Patterns.md",
		Category:   "resources",
		Tags:       []string{"ai", "patterns"},
	}

	chunks := ChunkMarkdown(body, meta)

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	// First chunk is the intro (no heading).
	if chunks[0].HeadingPath != "" {
		t.Errorf("expected empty heading path for intro, got %q", chunks[0].HeadingPath)
	}
	if !strings.Contains(chunks[0].Content, "Intro paragraph.") {
		t.Errorf("expected intro content, got %q", chunks[0].Content)
	}

	// Second chunk has heading.
	if chunks[1].HeadingPath != "## Section One" {
		t.Errorf("expected heading '## Section One', got %q", chunks[1].HeadingPath)
	}

	// Third chunk.
	if chunks[2].HeadingPath != "## Section Two" {
		t.Errorf("expected heading '## Section Two', got %q", chunks[2].HeadingPath)
	}
}

func TestChunkMarkdown_HeadingPathTracking(t *testing.T) {
	body := "## Vision\n\nVision intro.\n\n### Phase 0\n\nPhase zero content.\n\n### Phase 1\n\nPhase one content."
	meta := ChunkMeta{SourcePath: "test.md"}

	chunks := ChunkMarkdown(body, meta)

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	if chunks[0].HeadingPath != "## Vision" {
		t.Errorf("expected '## Vision', got %q", chunks[0].HeadingPath)
	}
	// Nested heading should include parent.
	if chunks[1].HeadingPath != "## Vision > ### Phase 0" {
		t.Errorf("expected '## Vision > ### Phase 0', got %q", chunks[1].HeadingPath)
	}
	if chunks[2].HeadingPath != "## Vision > ### Phase 1" {
		t.Errorf("expected '## Vision > ### Phase 1', got %q", chunks[2].HeadingPath)
	}
}

func TestChunkMarkdown_OversizedSection(t *testing.T) {
	// Create a section larger than maxTokens (1000 tokens ~ 4000 chars).
	para1 := strings.Repeat("Word ", 600) // 3000 chars
	para2 := strings.Repeat("More ", 600) // 3000 chars
	body := "## Big Section\n\n" + para1 + "\n\n" + para2

	meta := ChunkMeta{SourcePath: "test.md"}
	chunks := ChunkMarkdown(body, meta)

	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks for oversized section, got %d", len(chunks))
	}

	for i, c := range chunks {
		if c.TokenCount > maxTokens+50 { // small tolerance for prefix
			t.Errorf("chunk %d exceeds max tokens: %d", i, c.TokenCount)
		}
	}
}

func TestChunkMarkdown_ShortFile(t *testing.T) {
	body := "Just a short note."
	meta := ChunkMeta{SourcePath: "test.md"}

	chunks := ChunkMarkdown(body, meta)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].Content, "Just a short note.") {
		t.Errorf("expected content in chunk, got %q", chunks[0].Content)
	}
}

func TestChunkMarkdown_ContextPrefix(t *testing.T) {
	body := "## Implementation\n\nSome content."
	meta := ChunkMeta{
		SourcePath: "3 Resources/AI/Patterns.md",
		Category:   "resources",
		Tags:       []string{"ai", "patterns"},
	}

	chunks := ChunkMarkdown(body, meta)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}

	content := chunks[0].Content
	if !strings.Contains(content, "Source: 3 Resources/AI/Patterns.md") {
		t.Errorf("missing source in prefix")
	}
	if !strings.Contains(content, "Category: resources") {
		t.Errorf("missing category in prefix")
	}
	if !strings.Contains(content, "Tags: ai, patterns") {
		t.Errorf("missing tags in prefix")
	}
	if !strings.Contains(content, "Section: ## Implementation") {
		t.Errorf("missing section in prefix")
	}
	if !strings.Contains(content, "---\n") {
		t.Errorf("missing separator in prefix")
	}
}

func TestChunkJournal(t *testing.T) {
	body := "- [x] anker(build): something\n- [ ] next task"
	meta := ChunkMeta{
		SourcePath: "Journal/2026/04-April/2026-04-18-Saturday.md",
		Category:   "journal",
	}

	chunks := ChunkJournal(body, meta)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for journal, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].Content, "- [x] anker(build)") {
		t.Errorf("expected journal content, got %q", chunks[0].Content)
	}
}

func TestChunkInbox(t *testing.T) {
	body := "- [ ] First task\n- [ ] Second task\nNot a task\n- [x] Done task"
	meta := ChunkMeta{SourcePath: "Inbox.md", Category: "inbox"}

	chunks := ChunkInbox(body, meta)

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks for inbox tasks, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].Content, "- [ ] First task") {
		t.Errorf("expected first task, got %q", chunks[0].Content)
	}
}
