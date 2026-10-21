package pidfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	// DefaultPidFile 미지정 시 기본 PID 파일 경로
	DefaultPidFile = "/run/aria_state_sync.pid"
	// FallbackPidFile 권한 부족 등으로 인해 기본 경로에 저장할 수 없을 때 사용하는 대체 경로
	FallbackPidFile = "./run/aria_state_sync.pid"
)

// WritePID 설정된 경로 또는 기본 경로에 PID 파일을 저장합니다.
// 접근 권한이 없는 경우 자동으로 Fallback 경로(./run/aria_state_sync.pid)에 저장합니다.
// 최종 저장된 파일의 경로를 반환합니다.
func WritePID(configuredPath string) (string, error) {
	targetPath := strings.TrimSpace(configuredPath)
	if targetPath == "" {
		targetPath = DefaultPidFile
	}

	pid := os.Getpid()

	// 1차 시도: 대상 경로에 PID 저장
	err := writeToFile(targetPath, pid)
	if err == nil {
		return targetPath, nil
	}

	// 접근 권한 오류인 경우 fallback 경로 시도
	if isPermissionError(err) {
		fallbackPath := FallbackPidFile
		if cleanPath(targetPath) != cleanPath(fallbackPath) {
			if fbErr := writeToFile(fallbackPath, pid); fbErr == nil {
				return fallbackPath, nil
			} else {
				return "", fmt.Errorf("PID 파일 저장 실패 (기본 경로 %s 권한 오류: %v, 대체 경로 %s 오류: %w)", targetPath, err, fallbackPath, fbErr)
			}
		}
	}

	return "", fmt.Errorf("PID 파일 저장 실패 (%s): %w", targetPath, err)
}

// RemovePID 지정된 PID 파일을 삭제합니다.
// 파일 내 저장된 PID가 현재 프로세스 PID와 일치할 때만 안전하게 삭제합니다.
func RemovePID(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		// 읽을 수 없는 권한 오류 등이 아니라면 삭제 시도
		return os.Remove(path)
	}

	content := strings.TrimSpace(string(data))
	filePid, err := strconv.Atoi(content)
	if err == nil && filePid != os.Getpid() {
		// 다른 프로세스의 PID가 적혀있는 경우 삭제하지 않음
		return nil
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

func writeToFile(path string, pid int) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	content := []byte(fmt.Sprintf("%d\n", pid))
	return os.WriteFile(path, content, 0644)
}

func isPermissionError(err error) bool {
	if err == nil {
		return false
	}
	if os.IsPermission(err) {
		return true
	}
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}

func cleanPath(p string) string {
	abs, err := filepath.Abs(p)
	if err == nil {
		return abs
	}
	return filepath.Clean(p)
}

