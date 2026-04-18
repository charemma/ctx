package obsidian

import (
	"fmt"
	"strings"
)

const (
	targetTokens = 500
	maxTokens    = 1000
	charsPerToken = 4
)

// ChunkMeta holds contextual metadata prepended to each chunk for embedding.
type ChunkMeta struct {
	SourcePath   string
	Category     string
	Tags         []string
	HeadingPath  string
}

// ChunkResult represents a single chunk with its heading path and content.
type ChunkResult struct {
	Content     string
	HeadingPath string
	TokenCount  int
}

// ChunkMarkdown splits markdown body into chunks based on headings, with
// size-aware splitting for oversized sections.
func ChunkMarkdown(body string, meta ChunkMeta) []ChunkResult {
	sections := splitByHeadings(body)

	var results []ChunkResult
	for _, sec := range sections {
		prefix := contextPrefix(meta, sec.heading)
		chunks := splitToSize(sec.content, maxTokens-estimateTokens(prefix))
		for _, chunk := range chunks {
			full := prefix + chunk
			results = append(results, ChunkResult{
				Content:     full,
				HeadingPath: sec.heading,
				TokenCount:  estimateTokens(full),
			})
		}
	}
	return results
}

// ChunkJournal treats the entire journal file as a single chunk.
func ChunkJournal(body string, meta ChunkMeta) []ChunkResult {
	prefix := contextPrefix(meta, "")
	full := prefix + strings.TrimSpace(body)
	return []ChunkResult{{
		Content:     full,
		HeadingPath: "",
		TokenCount:  estimateTokens(full),
	}}
}

// ChunkInbox splits an inbox file into one chunk per task line.
func ChunkInbox(body string, meta ChunkMeta) []ChunkResult {
	lines := strings.Split(body, "\n")
	var results []ChunkResult
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- [") {
			continue
		}
		prefix := contextPrefix(meta, "")
		full := prefix + trimmed
		results = append(results, ChunkResult{
			Content:     full,
			HeadingPath: "",
			TokenCount:  estimateTokens(full),
		})
	}
	return results
}

type section struct {
	heading string
	content string
}

// splitByHeadings splits markdown into sections at ## and ### boundaries.
func splitByHeadings(body string) []section {
	lines := strings.Split(body, "\n")
	var sections []section
	var currentHeading string
	var currentLines []string

	flush := func() {
		text := strings.TrimSpace(strings.Join(currentLines, "\n"))
		if text != "" {
			sections = append(sections, section{
				heading: currentHeading,
				content: text,
			})
		}
	}

	headingStack := []string{} // tracks nested heading path

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "## ") && !strings.HasPrefix(trimmed, "### ") {
			flush()
			currentLines = nil
			title := strings.TrimPrefix(trimmed, "## ")
			headingStack = []string{trimmed[:len(trimmed)-len(title)] + title}
			currentHeading = strings.Join(headingStack, " > ")
			continue
		}

		if strings.HasPrefix(trimmed, "### ") {
			flush()
			currentLines = nil
			title := strings.TrimPrefix(trimmed, "### ")
			// Keep the parent ## heading if present.
			if len(headingStack) > 0 {
				parent := headingStack[0]
				headingStack = []string{parent, "### " + title}
			} else {
				headingStack = []string{"### " + title}
			}
			currentHeading = strings.Join(headingStack, " > ")
			continue
		}

		currentLines = append(currentLines, line)
	}
	flush()

	if len(sections) == 0 && strings.TrimSpace(body) != "" {
		sections = append(sections, section{content: strings.TrimSpace(body)})
	}
	return sections
}

func estimateTokens(s string) int {
	n := len(s) / charsPerToken
	if n == 0 && len(s) > 0 {
		return 1
	}
	return n
}

// splitToSize breaks text into pieces that fit within maxTok tokens.
// It tries paragraph boundaries first, then sentence boundaries.
func splitToSize(text string, maxTok int) []string {
	if estimateTokens(text) <= maxTok {
		return []string{text}
	}

	// Try splitting at paragraph boundaries.
	paragraphs := strings.Split(text, "\n\n")
	if len(paragraphs) > 1 {
		return mergeToSize(paragraphs, maxTok)
	}

	// Try splitting at sentence boundaries.
	sentences := splitSentences(text)
	if len(sentences) > 1 {
		return mergeToSize(sentences, maxTok)
	}

	// Last resort: hard split by character count.
	maxChars := maxTok * charsPerToken
	var result []string
	for len(text) > 0 {
		end := maxChars
		if end > len(text) {
			end = len(text)
		}
		result = append(result, text[:end])
		text = text[end:]
	}
	return result
}

// mergeToSize groups pieces together up to maxTok tokens each.
func mergeToSize(pieces []string, maxTok int) []string {
	var result []string
	var current []string
	currentTok := 0

	for _, p := range pieces {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		tok := estimateTokens(p)
		if currentTok+tok > maxTok && len(current) > 0 {
			result = append(result, strings.Join(current, "\n\n"))
			current = nil
			currentTok = 0
		}
		current = append(current, p)
		currentTok += tok
	}
	if len(current) > 0 {
		result = append(result, strings.Join(current, "\n\n"))
	}
	return result
}

// splitSentences does a simple sentence split on ". ", "! ", "? ".
func splitSentences(text string) []string {
	var sentences []string
	start := 0
	for i := 0; i < len(text)-1; i++ {
		if (text[i] == '.' || text[i] == '!' || text[i] == '?') && text[i+1] == ' ' {
			sentences = append(sentences, text[start:i+1])
			start = i + 2
		}
	}
	if start < len(text) {
		sentences = append(sentences, text[start:])
	}
	return sentences
}

func contextPrefix(meta ChunkMeta, headingPath string) string {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "Source: %s\n", meta.SourcePath)
	if meta.Category != "" {
		_, _ = fmt.Fprintf(&b, "Category: %s\n", meta.Category)
	}
	if len(meta.Tags) > 0 {
		_, _ = fmt.Fprintf(&b, "Tags: %s\n", strings.Join(meta.Tags, ", "))
	}
	if headingPath != "" {
		_, _ = fmt.Fprintf(&b, "Section: %s\n", headingPath)
	}
	_, _ = fmt.Fprintf(&b, "---\n")
	return b.String()
}
