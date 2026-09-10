package version

import (
	"fmt"
	"runtime"
)

var (
	// Version 은 애플리케이션 버전입니다. 빌드 시 ldflags로 덮어쓸 수 있습니다.
	Version = "1.0.0"
	// GitCommit 은 빌드 시점의 Git 커밋 해시입니다.
	GitCommit = "unknown"
	// BuildDate 는 빌드 시점 타임스탬프입니다.
	BuildDate = "unknown"
)

// Details 는 버전 상세 정보를 담는 구조체입니다.
type Details struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
	Compiler  string `json:"compiler"`
	Platform  string `json:"platform"`
}

// Get 은 버전 상세 정보를 반환합니다.
func Get() Details {
	return Details{
		Version:   Version,
		GitCommit: GitCommit,
		BuildDate: BuildDate,
		GoVersion: runtime.Version(),
		Compiler:  runtime.Compiler,
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}

// Info 는 사람이 읽기 쉬운 포맷의 버전 문자열을 반환합니다.
func Info() string {
	return fmt.Sprintf("state-sync %s (commit: %s, built: %s, go: %s)", Version, GitCommit, BuildDate, runtime.Version())
}
