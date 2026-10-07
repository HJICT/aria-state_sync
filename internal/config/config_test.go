package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfig_ConsoleSetting(t *testing.T) {
	tempDir := t.TempDir()

	// 1. console: true
	envContent := "AMI_PASSWORD=secret\nREDIS_PASSWORD=secret\n"
	if err := os.WriteFile(filepath.Join(tempDir, ".env.dev"), []byte(envContent), 0644); err != nil {
		t.Fatalf("failed to write .env.dev: %v", err)
	}

	yamlTrue := `
asterisk:
  host: "localhost"
  port: 5038
  user: "test"
  pass: "${AMI_PASSWORD}"
redis:
  host: "localhost"
  port: 6379
  pass: "${REDIS_PASSWORD}"
logs:
  level: "info"
  console: true
  path: "./logs"
`
	if err := os.WriteFile(filepath.Join(tempDir, "config.dev.yaml"), []byte(yamlTrue), 0644); err != nil {
		t.Fatalf("failed to write config.dev.yaml: %v", err)
	}

	cfg, err := LoadConfig("dev", tempDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.Logs.Console == nil || !*cfg.Logs.Console {
		t.Errorf("expected Logs.Console to be true, got %v", cfg.Logs.Console)
	}

	// 2. console: false
	yamlFalse := `
asterisk:
  host: "localhost"
  port: 5038
  user: "test"
  pass: "${AMI_PASSWORD}"
redis:
  host: "localhost"
  port: 6379
  pass: "${REDIS_PASSWORD}"
logs:
  level: "info"
  console: false
  path: "./logs"
`
	if err := os.WriteFile(filepath.Join(tempDir, "config.dev.yaml"), []byte(yamlFalse), 0644); err != nil {
		t.Fatalf("failed to write config.dev.yaml: %v", err)
	}

	cfg, err = LoadConfig("dev", tempDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.Logs.Console == nil || *cfg.Logs.Console {
		t.Errorf("expected Logs.Console to be false, got %v", cfg.Logs.Console)
	}

	// 3. console omitted (nil)
	yamlOmitted := `
asterisk:
  host: "localhost"
  port: 5038
  user: "test"
  pass: "${AMI_PASSWORD}"
redis:
  host: "localhost"
  port: 6379
  pass: "${REDIS_PASSWORD}"
logs:
  level: "info"
  path: "./logs"
`
	if err := os.WriteFile(filepath.Join(tempDir, "config.dev.yaml"), []byte(yamlOmitted), 0644); err != nil {
		t.Fatalf("failed to write config.dev.yaml: %v", err)
	}

	cfg, err = LoadConfig("dev", tempDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.Logs.Console != nil {
		t.Errorf("expected Logs.Console to be nil when omitted, got %v", *cfg.Logs.Console)
	}
}

func TestLoadConfig_MissingEnvVar(t *testing.T) {
	tempDir := t.TempDir()

	os.Unsetenv("REDIS_PASSWORD")
	os.Unsetenv("AMI_PASSWORD")

	// Missing REDIS_PASSWORD in .env
	envContent := "AMI_PASSWORD=secret\n"
	if err := os.WriteFile(filepath.Join(tempDir, ".env.dev"), []byte(envContent), 0644); err != nil {
		t.Fatalf("failed to write .env.dev: %v", err)
	}

	yaml := `
asterisk:
  host: "localhost"
  port: 5038
  user: "test"
  pass: "${AMI_PASSWORD}"
redis:
  host: "localhost"
  port: 6379
  pass: "${REDIS_PASSWORD}"
logs:
  level: "info"
`
	if err := os.WriteFile(filepath.Join(tempDir, "config.dev.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("failed to write config.dev.yaml: %v", err)
	}

	_, err := LoadConfig("dev", tempDir)
	if err == nil {
		t.Fatalf("expected error due to missing REDIS_PASSWORD, but got nil")
	}
}

func TestSyncConfig_GetInterval(t *testing.T) {
	// 1. nil config
	var sc *SyncConfig
	if sc.GetInterval() != time.Hour {
		t.Errorf("expected 1h for nil config, got %v", sc.GetInterval())
	}

	// 2. empty string
	sc = &SyncConfig{Interval: ""}
	if sc.GetInterval() != time.Hour {
		t.Errorf("expected 1h for empty interval, got %v", sc.GetInterval())
	}

	// 3. invalid duration
	sc = &SyncConfig{Interval: "invalid"}
	if sc.GetInterval() != time.Hour {
		t.Errorf("expected 1h for invalid interval, got %v", sc.GetInterval())
	}

	// 4. valid duration
	sc = &SyncConfig{Interval: "30m"}
	if sc.GetInterval() != 30*time.Minute {
		t.Errorf("expected 30m, got %v", sc.GetInterval())
	}
}
