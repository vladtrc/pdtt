package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadAppliesDefaultsAndValidates(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	secretPath := filepath.Join(dir, ".secret")
	if err := os.WriteFile(cfgPath, []byte(`
port: 9001
mysql:
  dsn: "user:pass@tcp(127.0.0.1:3306)/pdttweb?parseTime=true"
data_dir: /tmp/pdtt-data
admin_secret: test-admin-secret
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath, secretPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 9001 {
		t.Fatalf("port = %d", cfg.Port)
	}
	if cfg.RenderTimeout != defaultRenderTimeout {
		t.Fatalf("render_timeout = %s", cfg.RenderTimeout)
	}
	if cfg.Retention != defaultRetention {
		t.Fatalf("retention = %s", cfg.Retention)
	}
	if cfg.MaxSceneBytes != defaultMaxSceneBytes {
		t.Fatalf("max_scene_bytes = %d", cfg.MaxSceneBytes)
	}
	if cfg.OpenRouter.Timeout != defaultOpenRouterTimeout {
		t.Fatalf("openrouter.timeout = %s", cfg.OpenRouter.Timeout)
	}
	if cfg.OpenRouter.RulesPath != "" {
		t.Fatalf("openrouter.rules_path = %q, want empty embedded-docs default", cfg.OpenRouter.RulesPath)
	}
}

func TestLoadCapsRenderTimeout(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	secretPath := filepath.Join(dir, ".secret")
	if err := os.WriteFile(cfgPath, []byte(`
mysql:
  dsn: "user:pass@tcp(127.0.0.1:3306)/pdttweb?parseTime=true"
admin_secret: test-admin-secret
render_timeout: 30m
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath, secretPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RenderTimeout != 3*time.Minute {
		t.Fatalf("render_timeout = %s, want 3m", cfg.RenderTimeout)
	}
}

func TestValidateRejectsMissingAdminSecret(t *testing.T) {
	cfg := &Config{
		Port:          8080,
		MySQL:         MySQL{DSN: "dsn"},
		DataDir:       "./data",
		RenderTimeout: time.Minute,
		Retention:     time.Hour,
		MaxSceneBytes: 4096,
		CleanupEvery:  time.Hour,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsUnsafeAdminSecret(t *testing.T) {
	cfg := &Config{
		Port:          8080,
		MySQL:         MySQL{DSN: "dsn"},
		DataDir:       "./data",
		RenderTimeout: time.Minute,
		Retention:     time.Hour,
		MaxSceneBytes: 4096,
		AdminSecret:   "bad/secret",
		CleanupEvery:  time.Hour,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for slash in secret")
	}
}

func TestValidateRejectsInvalidDurations(t *testing.T) {
	base := Config{
		Port:          8080,
		MySQL:         MySQL{DSN: "dsn"},
		DataDir:       "./data",
		RenderTimeout: time.Minute,
		Retention:     time.Hour,
		MaxSceneBytes: 4096,
		AdminSecret:   "ok-secret",
		CleanupEvery:  time.Hour,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("base config: %v", err)
	}

	shortOpenRouterTimeout := base
	shortOpenRouterTimeout.OpenRouter.Timeout = time.Second
	if err := shortOpenRouterTimeout.Validate(); err == nil {
		t.Fatal("expected openrouter.timeout validation error")
	}
}
