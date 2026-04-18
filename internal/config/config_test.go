package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Database.URL != "postgres://localhost:5432/ctx?sslmode=disable" {
		t.Errorf("unexpected default database URL: %s", cfg.Database.URL)
	}
	if cfg.Embedding.Provider != "openai" {
		t.Errorf("unexpected default embedding provider: %s", cfg.Embedding.Provider)
	}
	if cfg.Embedding.Model != "text-embedding-3-small" {
		t.Errorf("unexpected default embedding model: %s", cfg.Embedding.Model)
	}
}

func TestLoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.yaml")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if cfg.Database.URL != "postgres://localhost:5432/ctx?sslmode=disable" {
		t.Errorf("expected default config, got database URL: %s", cfg.Database.URL)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ctx.yaml")

	content := `database:
  url: "postgres://custom:5432/mydb"
embedding:
  provider: ollama
  model: nomic-embed-text
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Database.URL != "postgres://custom:5432/mydb" {
		t.Errorf("unexpected database URL: %s", cfg.Database.URL)
	}
	if cfg.Embedding.Provider != "ollama" {
		t.Errorf("unexpected embedding provider: %s", cfg.Embedding.Provider)
	}
}

func TestEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ctx.yaml")

	content := `database:
  url: "postgres://file:5432/db"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CTX_DATABASE_URL", "postgres://env:5432/envdb")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Database.URL != "postgres://env:5432/envdb" {
		t.Errorf("expected env override, got: %s", cfg.Database.URL)
	}
}

func TestConfigDirFromEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CTX_HOME", dir)

	got, err := configDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Errorf("expected %s, got %s", dir, got)
	}
}
