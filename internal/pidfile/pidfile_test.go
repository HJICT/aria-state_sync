package pidfile

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestWritePID_Normal(t *testing.T) {
	tempDir := t.TempDir()
	pidPath := filepath.Join(tempDir, "test.pid")

	savedPath, err := WritePID(pidPath)
	if err != nil {
		t.Fatalf("WritePID failed: %v", err)
	}
	if savedPath != pidPath {
		t.Fatalf("expected savedPath %s, got %s", pidPath, savedPath)
	}

	data, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("failed to read pid file: %v", err)
	}

	savedPid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("failed to parse pid: %v", err)
	}
	if savedPid != os.Getpid() {
		t.Fatalf("expected pid %d, got %d", os.Getpid(), savedPid)
	}

	// Test RemovePID
	if err := RemovePID(savedPath); err != nil {
		t.Fatalf("RemovePID failed: %v", err)
	}
	if _, err := os.Stat(savedPath); !os.IsNotExist(err) {
		t.Fatalf("expected pid file to be removed, but still exists")
	}
}

func TestWritePID_PermissionFallback(t *testing.T) {
	tempDir := t.TempDir()
	// 쓰기 권한이 없는 디렉토리 생성
	noWriteDir := filepath.Join(tempDir, "nowrite")
	if err := os.Mkdir(noWriteDir, 0555); err != nil {
		t.Fatalf("failed to create read-only dir: %v", err)
	}
	defer os.Chmod(noWriteDir, 0755) // cleanup

	restrictedPath := filepath.Join(noWriteDir, "sub", "app.pid")

	savedPath, err := WritePID(restrictedPath)
	if err != nil {
		t.Fatalf("expected fallback to succeed, but got error: %v", err)
	}
	defer RemovePID(savedPath)

	expectedPath := filepath.Join(DefaultPidDir, "app.pid")
	if savedPath != expectedPath {
		t.Fatalf("expected fallback path %s, got %s", expectedPath, savedPath)
	}

	data, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("failed to read fallback pid file: %v", err)
	}

	savedPid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || savedPid != os.Getpid() {
		t.Fatalf("unexpected pid: %d (expected %d)", savedPid, os.Getpid())
	}
}

func TestWritePID_DefaultPath(t *testing.T) {
	// configuredPath가 빈 문자열일 때 DefaultPidFile("./run/state_sync.pid")에 바로 저장
	savedPath, err := WritePID("")
	if err != nil {
		t.Fatalf("WritePID with empty path failed: %v", err)
	}
	defer RemovePID(savedPath)

	if savedPath != DefaultPidFile {
		t.Fatalf("expected default path %s, got %s", DefaultPidFile, savedPath)
	}

	data, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("failed to read default pid file: %v", err)
	}

	savedPid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || savedPid != os.Getpid() {
		t.Fatalf("unexpected pid: %d (expected %d)", savedPid, os.Getpid())
	}
}

func TestRemovePID_DifferentPID(t *testing.T) {
	tempDir := t.TempDir()
	pidPath := filepath.Join(tempDir, "other.pid")

	// 다른 PID(999999) 기록
	if err := os.WriteFile(pidPath, []byte("999999\n"), 0644); err != nil {
		t.Fatalf("failed to write other pid file: %v", err)
	}

	// RemovePID 호출 시 다른 프로세스의 PID 파일이므로 삭제되지 않아야 함
	if err := RemovePID(pidPath); err != nil {
		t.Fatalf("RemovePID returned error: %v", err)
	}

	if _, err := os.Stat(pidPath); os.IsNotExist(err) {
		t.Fatalf("pid file should not have been deleted")
	}
}
