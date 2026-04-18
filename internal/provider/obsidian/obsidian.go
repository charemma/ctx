package obsidian

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charemma/ctx/internal/provider"
	"github.com/charemma/ctx/internal/store"
	"github.com/google/uuid"
)

// defaultExcludeDirs are directories always skipped during vault walking.
var defaultExcludeDirs = []string{
	".obsidian",
	".trash",
	".stfolder",
	"Templates",
}

var wikiLinkRe = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]+)?\]\]`)

// Provider syncs an Obsidian vault into the knowledge store.
type Provider struct {
	name     string
	vaultDir string
	excludes []string // additional glob patterns to exclude
	sourceID uuid.UUID
}

// New creates an Obsidian provider for the given vault directory.
func New(name, vaultDir string, excludes []string) *Provider {
	return &Provider{
		name:     name,
		vaultDir: vaultDir,
		excludes: excludes,
	}
}

func (p *Provider) Name() string { return p.name }
func (p *Provider) Type() string { return "obsidian" }

// Sync walks the vault, detects changes via content hashing, and upserts
// documents and chunks into the store.
func (p *Provider) Sync(ctx context.Context, st store.Store) (*provider.SyncStats, error) {
	stats := &provider.SyncStats{}

	// Ensure source exists.
	src, err := st.GetSourceByName(ctx, p.name)
	if err != nil {
		return nil, fmt.Errorf("getting source %q: %w", p.name, err)
	}
	p.sourceID = src.ID

	// Collect all markdown files from the vault.
	diskPaths, err := p.walkVault()
	if err != nil {
		return nil, fmt.Errorf("walking vault: %w", err)
	}
	stats.FilesFound = len(diskPaths)

	// Get existing document paths from the store for deletion detection.
	existingPaths, err := st.ListDocumentPaths(ctx, p.sourceID)
	if err != nil {
		return nil, fmt.Errorf("listing existing paths: %w", err)
	}

	diskSet := make(map[string]struct{}, len(diskPaths))
	for _, dp := range diskPaths {
		diskSet[dp] = struct{}{}
	}

	// Detect deleted files.
	var deletedPaths []string
	for _, ep := range existingPaths {
		if _, ok := diskSet[ep]; !ok {
			deletedPaths = append(deletedPaths, ep)
		}
	}
	if len(deletedPaths) > 0 {
		if err := st.DeleteDocumentsByPaths(ctx, p.sourceID, deletedPaths); err != nil {
			return nil, fmt.Errorf("deleting removed documents: %w", err)
		}
		stats.FilesDeleted = len(deletedPaths)
	}

	// Process each file.
	for _, relPath := range diskPaths {
		absPath := filepath.Join(p.vaultDir, relPath)
		data, err := os.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", relPath, err)
		}

		hash := contentHash(data)

		// Check for existing document.
		existing, err := st.GetDocumentByPath(ctx, p.sourceID, relPath)
		if err == nil && existing != nil && existing.ContentHash == hash {
			stats.FilesSkipped++
			continue
		}

		parsed := ParseFrontmatter(string(data))
		category := paraCategory(relPath)
		title := extractTitle(parsed, relPath)
		wikiLinks := extractWikiLinks(parsed.Body)

		metadata := map[string]any{}
		if len(wikiLinks) > 0 {
			metadata["wiki_links"] = wikiLinks
		}
		for k, v := range parsed.Frontmatter.Extra {
			metadata[k] = v
		}
		metaJSON, _ := json.Marshal(metadata)

		doc := &store.Document{
			SourceID:     p.sourceID,
			SourcePath:   relPath,
			Title:        strPtr(title),
			ContentHash:  hash,
			ContentType:  "text/markdown",
			ParaCategory: strPtr(category),
			Tags:         parsed.Frontmatter.Tags,
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
			return nil, fmt.Errorf("upserting document %s: %w", relPath, err)
		}

		// Delete old chunks, create new ones.
		if err := st.DeleteChunksByDocument(ctx, doc.ID); err != nil {
			return nil, fmt.Errorf("deleting chunks for %s: %w", relPath, err)
		}

		chunkMeta := ChunkMeta{
			SourcePath: relPath,
			Category:   category,
			Tags:       parsed.Frontmatter.Tags,
		}

		var chunkResults []ChunkResult
		switch {
		case category == "journal":
			chunkResults = ChunkJournal(parsed.Body, chunkMeta)
		case category == "inbox":
			chunkResults = ChunkInbox(parsed.Body, chunkMeta)
		default:
			chunkResults = ChunkMarkdown(parsed.Body, chunkMeta)
		}

		chunks := make([]store.Chunk, len(chunkResults))
		for i, cr := range chunkResults {
			chunks[i] = store.Chunk{
				ID:          uuid.New(),
				DocumentID:  doc.ID,
				ChunkIndex:  i,
				Content:     cr.Content,
				HeadingPath: strPtr(cr.HeadingPath),
				TokenCount:  intPtr(cr.TokenCount),
			}
		}
		if len(chunks) > 0 {
			if err := st.CreateChunks(ctx, chunks); err != nil {
				return nil, fmt.Errorf("creating chunks for %s: %w", relPath, err)
			}
			stats.ChunksCreated += len(chunks)
		}
	}

	return stats, nil
}

// walkVault returns relative paths of all .md files in the vault,
// skipping excluded directories and patterns.
func (p *Provider) walkVault() ([]string, error) {
	var paths []string

	err := filepath.WalkDir(p.vaultDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(p.vaultDir, path)
		if err != nil {
			return err
		}

		if d.IsDir() {
			name := d.Name()
			for _, exc := range defaultExcludeDirs {
				if name == exc {
					return filepath.SkipDir
				}
			}
			return nil
		}

		if filepath.Ext(path) != ".md" {
			return nil
		}

		if p.isExcluded(rel) {
			return nil
		}

		paths = append(paths, rel)
		return nil
	})

	return paths, err
}

// isExcluded checks whether a relative path matches any configured exclude pattern.
func (p *Provider) isExcluded(relPath string) bool {
	for _, pattern := range p.excludes {
		matched, err := filepath.Match(pattern, relPath)
		if err == nil && matched {
			return true
		}
		// Also try matching against just the filename.
		matched, err = filepath.Match(pattern, filepath.Base(relPath))
		if err == nil && matched {
			return true
		}
	}
	return false
}

// paraCategory determines the PARA category from the relative path.
func paraCategory(relPath string) string {
	parts := strings.SplitN(relPath, string(filepath.Separator), 2)
	if len(parts) == 0 {
		return "other"
	}

	top := parts[0]
	switch {
	case strings.HasPrefix(top, "1 Projects"):
		return "projects"
	case strings.HasPrefix(top, "2 Areas"):
		return "areas"
	case strings.HasPrefix(top, "3 Resources"):
		return "resources"
	case strings.HasPrefix(top, "4 Archive"):
		return "archive"
	case strings.HasPrefix(top, "Journal"):
		return "journal"
	case relPath == "Inbox.md":
		return "inbox"
	default:
		return "other"
	}
}

// extractTitle derives a document title from frontmatter, first heading, or filename.
func extractTitle(parsed ParseResult, relPath string) string {
	// Check frontmatter for title.
	if t, ok := parsed.Frontmatter.Extra["title"]; ok && t != "" {
		return t
	}

	// Look for first # heading in body.
	for _, line := range strings.Split(parsed.Body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "## ") {
			return strings.TrimPrefix(trimmed, "# ")
		}
	}

	// Fall back to filename without extension.
	base := filepath.Base(relPath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// extractWikiLinks finds all [[target]] references in the body.
func extractWikiLinks(body string) []string {
	matches := wikiLinkRe.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	var links []string
	for _, m := range matches {
		target := strings.TrimSpace(m[1])
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		links = append(links, target)
	}
	return links
}

func contentHash(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
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
