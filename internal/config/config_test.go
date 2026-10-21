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

func TestLoadConfig_PidFile(t *testing.T) {
	tempDir := t.TempDir()

	envContent := "AMI_PASSWORD=secret\nREDIS_PASSWORD=secret\n"
	if err := os.WriteFile(filepath.Join(tempDir, ".env.dev"), []byte(envContent), 0644); err != nil {
		t.Fatalf("failed to write .env.dev: %v", err)
	}

	// 1. pid_file 지정된 경우
	yamlWithPid := `
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
pid_file: "/custom/path/app.pid"
`
	if err := os.WriteFile(filepath.Join(tempDir, "config.dev.yaml"), []byte(yamlWithPid), 0644); err != nil {
		t.Fatalf("failed to write config.dev.yaml: %v", err)
	}

	cfg, err := LoadConfig("dev", tempDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.PidFile != "/custom/path/app.pid" {
		t.Errorf("expected PidFile to be '/custom/path/app.pid', got %s", cfg.PidFile)
	}

	// 2. 레거시 pidFile 호환성 확인
	yamlWithLegacyPid := `
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
pidFile: "/legacy/path/app.pid"
`
	if err := os.WriteFile(filepath.Join(tempDir, "config.dev.yaml"), []byte(yamlWithLegacyPid), 0644); err != nil {
		t.Fatalf("failed to write config.dev.yaml: %v", err)
	}

	cfg, err = LoadConfig("dev", tempDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.PidFile != "/legacy/path/app.pid" {
		t.Errorf("expected PidFile to be '/legacy/path/app.pid', got %s", cfg.PidFile)
	}

	// 3. pid_file 생략된 경우
	yamlWithoutPid := `
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
	if err := os.WriteFile(filepath.Join(tempDir, "config.dev.yaml"), []byte(yamlWithoutPid), 0644); err != nil {
		t.Fatalf("failed to write config.dev.yaml: %v", err)
	}

	cfg, err = LoadConfig("dev", tempDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.PidFile != "" {
		t.Errorf("expected PidFile to be empty, got %s", cfg.PidFile)
	}
}

func TestResolveLogPath(t *testing.T) {
	tempDir := t.TempDir()

	// 1. 지정된 경로 + 쓰기 권한 있음
	customPath := filepath.Join(tempDir, "custom_log")
	resolved := ResolveLogPath(customPath)
	if resolved != customPath {
		t.Errorf("expected custom path %s, got %s", customPath, resolved)
	}

	// 2. 지정된 경로 + 쓰기 권한 없음 (read-only 디렉토리 하위)
	noWriteDir := filepath.Join(tempDir, "nowrite")
	if err := os.Mkdir(noWriteDir, 0555); err != nil {
		t.Fatalf("failed to create read-only dir: %v", err)
	}
	defer os.Chmod(noWriteDir, 0755)

	restrictedPath := filepath.Join(noWriteDir, "sub", "test.log")
	resolved = ResolveLogPath(restrictedPath)
	if resolved != DefaultWorkingLogPath {
		t.Errorf("expected fallback to %s on permission error, got %s", DefaultWorkingLogPath, resolved)
	}

	// 3. 미지정 + 비 systemd 환경
	t.Setenv("INVOCATION_ID", "")
	resolved = ResolveLogPath("")
	if resolved != DefaultWorkingLogPath {
		t.Errorf("expected default working log path %s, got %s", DefaultWorkingLogPath, resolved)
	}

	// 4. 미지정 + systemd 환경 (INVOCATION_ID 존재)
	t.Setenv("INVOCATION_ID", "mock-systemd-invocation-id")
	resolved = ResolveLogPath("")
	if os.Geteuid() != 0 {
		// 일반 사용자 환경에서는 /var/log/aria-state-sync 권한이 없으므로 ./log 로 fallback 되어야 함
		if resolved != DefaultWorkingLogPath {
			t.Errorf("expected fallback %s for non-root in systemd, got %s", DefaultWorkingLogPath, resolved)
		}
	} else {
		// root 환경에서는 /var/log/aria-state-sync 사용
		if resolved != DefaultSystemdLogPath {
			t.Errorf("expected %s for root in systemd, got %s", DefaultSystemdLogPath, resolved)
		}
	}
}
