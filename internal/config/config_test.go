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
log:
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
	if cfg.Log.Console == nil || !*cfg.Log.Console {
		t.Errorf("expected Log.Console to be true, got %v", cfg.Log.Console)
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
log:
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
	if cfg.Log.Console == nil || *cfg.Log.Console {
		t.Errorf("expected Log.Console to be false, got %v", cfg.Log.Console)
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
log:
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
	if cfg.Log.Console != nil {
		t.Errorf("expected Log.Console to be nil when omitted, got %v", *cfg.Log.Console)
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
log:
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
log:
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
log:
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
log:
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
	if resolved != DefaultLogPath {
		t.Errorf("expected fallback to %s on permission error, got %s", DefaultLogPath, resolved)
	}

	// 3. 미지정 (빈 문자열)
	resolved = ResolveLogPath("")
	if resolved != DefaultLogPath {
		t.Errorf("expected default log path %s, got %s", DefaultLogPath, resolved)
	}

	// 4. 공백만 지정된 경우
	resolved = ResolveLogPath("   ")
	if resolved != DefaultLogPath {
		t.Errorf("expected default log path %s for whitespace, got %s", DefaultLogPath, resolved)
	}
}

func TestResolveLogPathAndFile(t *testing.T) {
	tempDir := t.TempDir()

	// 1. 경로만 지정된 경우 -> 파일명은 기본값 "state_sync.log"
	dirOnly := filepath.Join(tempDir, "only_dir")
	dir, file := ResolveLogPathAndFile(dirOnly)
	if dir != dirOnly {
		t.Errorf("expected dir %s, got %s", dirOnly, dir)
	}
	if file != DefaultLogFilename {
		t.Errorf("expected file %s, got %s", DefaultLogFilename, file)
	}

	// 2. 경로만 지정 (끝에 슬래시 포함)
	dirWithSlash := filepath.Join(tempDir, "slash_dir") + "/"
	dir, file = ResolveLogPathAndFile(dirWithSlash)
	if dir != dirWithSlash {
		t.Errorf("expected dir %s, got %s", dirWithSlash, dir)
	}
	if file != DefaultLogFilename {
		t.Errorf("expected file %s, got %s", DefaultLogFilename, file)
	}

	// 3. 파일명까지 정의된 경우 -> 해당 파일명 및 상위 디렉토리 사용
	customLogPath := filepath.Join(tempDir, "service_dir", "my_app.log")
	dir, file = ResolveLogPathAndFile(customLogPath)
	expectedDir := filepath.Dir(customLogPath)
	if dir != expectedDir {
		t.Errorf("expected dir %s, got %s", expectedDir, dir)
	}
	if file != "my_app.log" {
		t.Errorf("expected file 'my_app.log', got %s", file)
	}

	// 4. 파일명까지 정의되었으나 해당 위치에 권한이 없는 경우 -> fallback 디렉토리("./log") + 지정된 파일명 유지
	noWriteDir := filepath.Join(tempDir, "nowrite")
	if err := os.Mkdir(noWriteDir, 0555); err != nil {
		t.Fatalf("failed to create read-only dir: %v", err)
	}
	defer os.Chmod(noWriteDir, 0755)

	restrictedWithFile := filepath.Join(noWriteDir, "sub", "app.log")
	dir, file = ResolveLogPathAndFile(restrictedWithFile)
	if dir != DefaultLogPath {
		t.Errorf("expected fallback dir %s, got %s", DefaultLogPath, dir)
	}
	if file != "app.log" {
		t.Errorf("expected file 'app.log', got %s", file)
	}

	// 5. 미지정 (빈 문자열 / 공백)
	dir, file = ResolveLogPathAndFile("")
	if dir != DefaultLogPath || file != DefaultLogFilename {
		t.Errorf("expected %s/%s for empty, got %s/%s", DefaultLogPath, DefaultLogFilename, dir, file)
	}

	dir, file = ResolveLogPathAndFile("   ")
	if dir != DefaultLogPath || file != DefaultLogFilename {
		t.Errorf("expected %s/%s for whitespace, got %s/%s", DefaultLogPath, DefaultLogFilename, dir, file)
	}
}
