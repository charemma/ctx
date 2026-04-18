package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds user configuration for ctx.
type Config struct {
	Database  DatabaseConfig  `yaml:"database"`
	Embedding EmbeddingConfig `yaml:"embedding"`
	Sources   []SourceConfig  `yaml:"sources"`
}

// DatabaseConfig holds database connection settings.
type DatabaseConfig struct {
	URL string `yaml:"url"`
}

// EmbeddingConfig holds embedding provider settings.
type EmbeddingConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

// SourceConfig holds a single source definition.
type SourceConfig struct {
	Type     string            `yaml:"type"`
	Location string            `yaml:"location"`
	Metadata map[string]string `yaml:"metadata,omitempty"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		Database: DatabaseConfig{
			URL: "postgres://localhost:5432/ctx?sslmode=disable",
		},
		Embedding: EmbeddingConfig{
			Provider: "openai",
			Model:    "text-embedding-3-small",
		},
		Sources: []SourceConfig{},
	}
}

// configDir returns the configuration directory, respecting CTX_HOME.
func configDir() (string, error) {
	if dir := os.Getenv("CTX_HOME"); dir != "" {
		return dir, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "ctx"), nil
}

// Load reads configuration from the given path, or the default location.
// Environment variables override file values:
//   - CTX_HOME: config directory (default: ~/.config/ctx)
//   - CTX_DATABASE_URL: overrides database.url
//   - OPENAI_API_KEY: used by embedding provider at runtime
func Load(path string) (*Config, error) {
	if path == "" {
		dir, err := configDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(dir, "ctx.yaml")
	}

	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyEnvOverrides(cfg)
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	applyEnvOverrides(cfg)
	return cfg, nil
}

// applyEnvOverrides applies environment variable overrides to the config.
func applyEnvOverrides(cfg *Config) {
	if url := os.Getenv("CTX_DATABASE_URL"); url != "" {
		cfg.Database.URL = url
	}
}
