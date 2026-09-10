package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// Config 프로젝트의 전체 설정을 담고 있는 루트 구조체입니다.
type Config struct {
	Asterisk AsteriskConfig `yaml:"asterisk" mapstructure:"asterisk"` // Asterisk AMI 연결 설정
	Redis    RedisConfig    `yaml:"redis" mapstructure:"redis"`       // Redis 데이터베이스 설정
	Logs     LogConfig      `yaml:"logs" mapstructure:"logs"`         // 로그 기록 설정
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
	envPath := filepath.Join(configPath, ".env")
	if err := godotenv.Load(envPath); err != nil {
		return nil, fmt.Errorf(".env 파일을 찾을 수 없음: %w", err)
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
