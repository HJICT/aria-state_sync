package pidfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// DefaultPidDir 미지정 또는 fallback 시 기본 PID 디렉토리
	DefaultPidDir = "./run"
	// DefaultPidFile 미지정 시 기본 PID 파일 경로
	DefaultPidFile = "./run/state_sync.pid"
)

// WritePID 설정된 경로 또는 기본 경로에 PID 파일을 저장합니다.
// 경로가 지정되지 않았거나 지정된 경로에 권한이 없는 경우 Working directory 하위 run 디렉토리에 저장합니다.
// 최종 저장된 파일의 경로를 반환합니다.
func WritePID(configuredPath string) (string, error) {
	targetPath := strings.TrimSpace(configuredPath)
	pid := os.Getpid()

	// 1. 경로가 미지정된 경우: 기본 경로(./run/state_sync.pid)에 바로 저장
	if targetPath == "" {
		if err := writeToFile(DefaultPidFile, pid); err != nil {
			return "", fmt.Errorf("PID 파일 저장 실패 (%s): %w", DefaultPidFile, err)
		}
		return DefaultPidFile, nil
	}

	// 2. 경로가 지정된 경우: 해당 경로 저장 시도
	err := writeToFile(targetPath, pid)
	if err == nil {
		return targetPath, nil
	}

	// 권한 부족 등의 이유로 실패한 경우 Working directory 하위 run 디렉토리에 생성
	fallbackFile := filepath.Base(targetPath)
	if fallbackFile == "." || fallbackFile == "/" || fallbackFile == "" {
		fallbackFile = "state_sync.pid"
	}
	fallbackPath := filepath.Join(DefaultPidDir, fallbackFile)

	if fbErr := writeToFile(fallbackPath, pid); fbErr == nil {
		return fallbackPath, nil
	}

	return "", fmt.Errorf("PID 파일 저장 실패 (지정 경로 %s 오류: %v, 대체 경로 %s 오류)", targetPath, err, fallbackPath)
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
