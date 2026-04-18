package obsidian

import (
	"testing"
)

func TestParseFrontmatter_WithTags(t *testing.T) {
	content := "---\ntags:\n  - journal\n  - anker\n---\nSome body content."

	result := ParseFrontmatter(content)

	if len(result.Frontmatter.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(result.Frontmatter.Tags))
	}
	if result.Frontmatter.Tags[0] != "journal" {
		t.Errorf("expected tag 'journal', got %q", result.Frontmatter.Tags[0])
	}
	if result.Frontmatter.Tags[1] != "anker" {
		t.Errorf("expected tag 'anker', got %q", result.Frontmatter.Tags[1])
	}
	if result.Body != "Some body content." {
		t.Errorf("unexpected body: %q", result.Body)
	}
}

func TestParseFrontmatter_WithDate(t *testing.T) {
	content := "---\ndate: 2026-04-18\nstatus: accepted\n---\nBody here."

	result := ParseFrontmatter(content)

	if result.Frontmatter.Date != "2026-04-18" {
		t.Errorf("expected date '2026-04-18', got %q", result.Frontmatter.Date)
	}
	if result.Frontmatter.Status != "accepted" {
		t.Errorf("expected status 'accepted', got %q", result.Frontmatter.Status)
	}
	if result.Body != "Body here." {
		t.Errorf("unexpected body: %q", result.Body)
	}
}

func TestParseFrontmatter_WithoutFrontmatter(t *testing.T) {
	content := "# Just a heading\n\nSome text."

	result := ParseFrontmatter(content)

	if len(result.Frontmatter.Tags) != 0 {
		t.Errorf("expected no tags, got %d", len(result.Frontmatter.Tags))
	}
	if result.Body != content {
		t.Errorf("expected body to equal full content, got %q", result.Body)
	}
}

func TestParseFrontmatter_EmptyFile(t *testing.T) {
	result := ParseFrontmatter("")

	if len(result.Frontmatter.Tags) != 0 {
		t.Errorf("expected no tags, got %d", len(result.Frontmatter.Tags))
	}
	if result.Body != "" {
		t.Errorf("expected empty body, got %q", result.Body)
	}
}

func TestParseFrontmatter_ExtraFields(t *testing.T) {
	content := "---\ntags:\n  - test\ncustom_field: hello\n---\nBody."

	result := ParseFrontmatter(content)

	if v, ok := result.Frontmatter.Extra["custom_field"]; !ok || v != "hello" {
		t.Errorf("expected extra field 'custom_field' = 'hello', got %v", result.Frontmatter.Extra)
	}
}
