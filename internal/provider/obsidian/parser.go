package obsidian

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// Frontmatter holds parsed YAML frontmatter fields.
type Frontmatter struct {
	Tags   []string          `yaml:"tags"`
	Date   string            `yaml:"date"`
	Status string            `yaml:"status"`
	Extra  map[string]string `yaml:"-"`
}

// ParseResult holds the parsed frontmatter and the remaining body content.
type ParseResult struct {
	Frontmatter Frontmatter
	Body        string
}

// ParseFrontmatter splits a markdown file into YAML frontmatter and body.
// Returns the parsed frontmatter and the content after the closing --- delimiter.
func ParseFrontmatter(content string) ParseResult {
	if !strings.HasPrefix(content, "---") {
		return ParseResult{Body: content}
	}

	// Find closing delimiter (must be on its own line after the opening one).
	rest := content[3:]
	rest = strings.TrimLeft(rest, " \t")
	if len(rest) == 0 || rest[0] != '\n' {
		return ParseResult{Body: content}
	}
	rest = rest[1:]

	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return ParseResult{Body: content}
	}

	yamlBlock := rest[:idx]
	body := rest[idx+4:] // skip "\n---"
	body = strings.TrimLeft(body, "\r\n")

	var fm Frontmatter
	_ = yaml.Unmarshal([]byte(yamlBlock), &fm)

	// Also unmarshal into a generic map for extra fields.
	var raw map[string]any
	_ = yaml.Unmarshal([]byte(yamlBlock), &raw)
	fm.Extra = make(map[string]string)
	for k, v := range raw {
		switch k {
		case "tags", "date", "status":
			continue
		default:
			if s, ok := v.(string); ok {
				fm.Extra[k] = s
			}
		}
	}

	return ParseResult{
		Frontmatter: fm,
		Body:        body,
	}
}
