package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// Config 프로젝트의 전체 설정을 담고 있는 루트 구조체입니다.
type Config struct {
	Asterisk AsteriskConfig `yaml:"asterisk" mapstructure:"asterisk"` // Asterisk AMI 연결 설정
	Redis    RedisConfig    `yaml:"redis" mapstructure:"redis"`       // Redis 데이터베이스 설정
	Sync     SyncConfig     `yaml:"sync" mapstructure:"sync"`         // 상태 동기화 및 주기 설정
	Log      LogConfig      `yaml:"logs" mapstructure:"log"`          // 로그 기록 설정
	PidFile  string         `yaml:"pid_file" mapstructure:"pid_file"` // PID 파일 저장 경로
}

// UnmarshalYAML yaml.v3 언마샬링 시 pid_file/pidFile 및 log/logs 설정을 모두 유연하게 지원합니다.
func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	type rawConfig Config
	var raw rawConfig
	if err := node.Decode(&raw); err != nil {
		return err
	}
	*c = Config(raw)

	// 하위 호환성 보정
	var fallback struct {
		LegacyPidFile string    `yaml:"pidFile"`
		AltLog        LogConfig `yaml:"log"`
	}
	if err := node.Decode(&fallback); err == nil {
		if c.PidFile == "" && fallback.LegacyPidFile != "" {
			c.PidFile = fallback.LegacyPidFile
		}
		// logs 키가 없고 log 키로 작성된 경우 보정
		if c.Log.Level == "" && fallback.AltLog.Level != "" {
			c.Log = fallback.AltLog
		}
		if c.Log.Console == nil && fallback.AltLog.Console != nil {
			c.Log.Console = fallback.AltLog.Console
		}
		if c.Log.Path == "" && fallback.AltLog.Path != "" {
			c.Log.Path = fallback.AltLog.Path
		}
	}
	return nil
}

// SyncConfig 상태 동기화 주기 및 정책 설정입니다.
type SyncConfig struct {
	Interval string `yaml:"interval" mapstructure:"interval"` // 주기적 동기화 간격 (기본값: "1h")
}

// GetInterval 주기적 동기화 간격을 time.Duration으로 반환합니다. (기본값: 1시간)
func (s *SyncConfig) GetInterval() time.Duration {
	if s == nil || s.Interval == "" {
		return time.Hour
	}
	d, err := time.ParseDuration(s.Interval)
	if err != nil || d <= 0 {
		return time.Hour
	}
	return d
}

// AsteriskConfig Asterisk Manager Interface(AMI) 연결을 위한 상세 설정입니다.
type AsteriskConfig struct {
	Host string `yaml:"host" mapstructure:"host"` // Asterisk 서버 IP 주소
	Port int    `yaml:"port" mapstructure:"port"` // AMI 서비스 포트 (기본: 5038)
	User string `yaml:"user" mapstructure:"user"` // AMI 접속 사용자 아이디
	Pass string `yaml:"pass" mapstructure:"pass"` // AMI 접속 비밀번호 (환경 변수 치환 가능)
}

// RedisConfig 상태 정보 저장을 위한 Redis 연결 상세 설정입니다.
type RedisConfig struct {
	Host string `yaml:"host" mapstructure:"host"` // Redis 서버 주소
	Port int    `yaml:"port" mapstructure:"port"` // Redis 서비스 포트
	Pass string `yaml:"pass" mapstructure:"pass"` // Redis 접속 비밀번호
}

// LogConfig 애플리케이션 로그 기록 및 관리 정책 설정입니다.
type LogConfig struct {
	Level      string `yaml:"level" mapstructure:"level"`             // 로그 레벨 (debug, info, warn, error)
	Console    *bool  `yaml:"console" mapstructure:"console"`         // 콘솔(Stdout) 출력 여부 (미지정 시 기본값: true)
	Path       string `yaml:"path" mapstructure:"path"`               // 로그 파일 저장 경로
	Rotation   string `yaml:"rotation" mapstructure:"rotation"`       // 로그 롤링 방식 (daily, size)
	MaxSize    int    `yaml:"max_size" mapstructure:"max_size"`       // 파일당 최대 크기 (MB)
	MaxBackups int    `yaml:"max_backups" mapstructure:"max_backups"` // 보관할 최대 백업 파일 개수
	MaxAge     int    `yaml:"max_age" mapstructure:"max_age"`         // 로그 보관 기간 (일)
	Compress   bool   `yaml:"compress" mapstructure:"compress"`       // 로그 파일 압축 여부
}

const (
	// DefaultLogPath 미지정 시 또는 권한 부족 시 기본 로그 저장 경로 (Working directory 하위 log)
	DefaultLogPath = "./log"
	// DefaultLogFilename 로그 경로만 지정되거나 파일명이 미지정된 경우 기본 로그 파일명
	DefaultLogFilename = "state_sync.log"
)

// ResolveLogPathAndFile 설정된 logs.path 값으로부터 저장 디렉토리와 파일명을 결정합니다.
// 1. 설정 경로에 파일명(확장자 포함)이 포함되어 있으면 해당 파일명을 사용하고 디렉토리를 분리합니다.
// 2. 설정 경로에 경로만 지정되어 있으면 파일명은 기본값 "state_sync.log"를 사용합니다.
// 3. 디렉토리에 쓰기 권한이 없거나 미지정된 경우 Working directory 하위 log ("./log")에 저장합니다.
func ResolveLogPathAndFile(configuredPath string) (dirPath string, filename string) {
	trimmed := strings.TrimSpace(configuredPath)
	if trimmed == "" {
		return DefaultLogPath, DefaultLogFilename
	}

	targetDir := trimmed
	targetFile := DefaultLogFilename

	// 슬래시로 끝나지 않고 확장자가 있는 경우 파일명이 포함된 경로로 판단
	hasTrailingSlash := strings.HasSuffix(trimmed, "/") || strings.HasSuffix(trimmed, "\\")
	if !hasTrailingSlash && filepath.Ext(trimmed) != "" {
		targetFile = filepath.Base(trimmed)
		targetDir = filepath.Dir(trimmed)
		if targetDir == "." || targetDir == "" {
			targetDir = DefaultLogPath
		}
	}

	// 디렉토리 쓰기 권한 확인
	if canWrite(targetDir) {
		return targetDir, targetFile
	}

	// 권한 부족 시 Working directory 하위 log로 대체
	return DefaultLogPath, targetFile
}

// ResolveLogPath 로그 저장 디렉토리 경로를 결정합니다.
func ResolveLogPath(configuredPath string) string {
	dir, _ := ResolveLogPathAndFile(configuredPath)
	return dir
}

func canWrite(dir string) bool {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false
	}
	testFile := filepath.Join(dir, fmt.Sprintf(".perm_test_%d_%d", os.Getpid(), time.Now().UnixNano()))
	f, err := os.OpenFile(testFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(testFile)
	return true
}

// LoadConfig 지정된 경로에서 설정 파일을 읽어오고 환경 변수를 적용하여 반환합니다.
// 1. .env 환경 파일 로드 (godotenv)
// 2. config.{env}.yaml 파일 읽기
// 3. 환경 변수 치환 (${VAR} 형식 지원)
// 4. gopkg.in/yaml.v3를 사용하여 구조체로 언마샬링
func LoadConfig(appEnv, configPath string) (*Config, error) {

	// 설정파일명 설정
	cfgFile := "config.yaml"
	if appEnv != "" && appEnv != "prod" {
		cfgFile = fmt.Sprintf("config.%s.yaml", appEnv)
	}

	targetPath := configPath
	lowerPath := strings.ToLower(configPath)
	if strings.HasSuffix(lowerPath, ".yaml") || strings.HasSuffix(lowerPath, ".yml") {
		targetPath = configPath
	} else {
		targetPath = filepath.Join(configPath, cfgFile)
	}

	// .env 파일 로드
	baseDir := configPath
	if strings.HasSuffix(lowerPath, ".yaml") || strings.HasSuffix(lowerPath, ".yml") {
		baseDir = filepath.Dir(configPath)
	}

	envFile := ".env"
	if appEnv != "" && appEnv != "prod" {
		specificEnv := filepath.Join(baseDir, fmt.Sprintf(".env.%s", appEnv))
		if _, err := os.Stat(specificEnv); err == nil {
			envFile = fmt.Sprintf(".env.%s", appEnv)
		}
	}
	envPath := filepath.Join(baseDir, envFile)
	if err := godotenv.Load(envPath); err != nil {
		// .env 파일이 없는 것은 환경 변수(systemd EnvironmentFile 등)로 직접 제공될 수 있으므로 허용
	}

	// yaml 파일 로드
	data, err := os.ReadFile(targetPath)
	if err != nil {
		return nil, fmt.Errorf("설정 파일을 읽는 중 오류 발생: %w", err)
	}

	// [비즈니스 로직] 환경 변수 치환
	// YAML 파일 내에 "${AMI_PASSWORD}"와 같이 저장된 문자열을 실제 OS 환경 변수 값으로 변경합니다.
	var missingVars []string
	expanded := os.Expand(string(data), func(k string) string {
		v, ok := os.LookupEnv(k)
		if !ok {
			missingVars = append(missingVars, k)
			return ""
		}
		return v
	})
	if len(missingVars) > 0 {
		return nil, fmt.Errorf("설정 파일의 환경 변수가 치환되지 않았습니다: %s", strings.Join(missingVars, ", "))
	}

	// 최종적으로 설정 객체에 매핑
	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("설정 데이터를 구조체로 변환하는 중 오류 발생: %w", err)
	}

	return &cfg, nil
}
