package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

// Config 프로젝트의 전체 설정을 담고 있는 루트 구조체입니다.
type Config struct {
	Asterisk AsteriskConfig `mapstructure:"asterisk"` // Asterisk AMI 연결 설정
	Redis    RedisConfig    `mapstructure:"redis"`    // Redis 데이터베이스 설정
	Logs     LogConfig      `mapstructure:"logs"`     // 로그 기록 설정
}

// AsteriskConfig Asterisk Manager Interface(AMI) 연결을 위한 상세 설정입니다.
type AsteriskConfig struct {
	Host string `mapstructure:"host"` // Asterisk 서버 IP 주소
	Port int    `mapstructure:"port"` // AMI 서비스 포트 (기본: 5038)
	User string `mapstructure:"user"` // AMI 접속 사용자 아이디
	Pass string `mapstructure:"pass"` // AMI 접속 비밀번호 (환경 변수 치환 가능)
}

// RedisConfig 상태 정보 저장을 위한 Redis 연결 상세 설정입니다.
type RedisConfig struct {
	Host string `mapstructure:"host"` // Redis 서버 주소
	Port int    `mapstructure:"port"` // Redis 서비스 포트
	Pass string `mapstructure:"pass"` // Redis 접속 비밀번호
}

// LogConfig 애플리케이션 로그 기록 및 관리 정책 설정입니다.
type LogConfig struct {
	Level      string `mapstructure:"level"`       // 로그 레벨 (debug, info, warn, error)
	Path       string `mapstructure:"path"`        // 로그 파일 저장 경로
	Rotation   string `mapstructure:"rotation"`    // 로그 롤링 방식 (daily, size)
	MaxSize    int    `mapstructure:"max_size"`    // 파일당 최대 크기 (MB)
	MaxBackups int    `mapstructure:"max_backups"` // 보관할 최대 백업 파일 개수
	MaxAge     int    `mapstructure:"max_age"`     // 로그 보관 기간 (일)
	Compress   bool   `mapstructure:"compress"`    // 로그 파일 압축 여부
}

// LoadConfig 지정된 경로에서 설정 파일을 읽어오고 환경 변수를 적용하여 반환합니다.
// 1. Viper 객체 생성 및 설정 파일 위치 지정
// 2. 환경 변수 자동 매핑 설정 (점(.)을 언더바(_)로 치환하여 매칭)
// 3. 설정 파일 읽기 및 환경 변수 치환 (${VAR} 형식 지원)
// 4. 구조체로 언마샬링(Unmarshal) 하여 반환
func LoadConfig(appEnv string, configPath *string) (*Config, error) {

	// 설정파일명 설정
	envFile := fmt.Sprintf(".env.%s", appEnv)
	configFile := fmt.Sprintf("config.%s.yaml", appEnv)

	var configFilePath, envFilePath string
	if configPath != nil && *configPath != "" {
		envFilePath = filepath.Join(*configPath, envFile)
		configFilePath = filepath.Join(*configPath, configFile)
	} else {
		envFilePath = filepath.Join("./configs", envFile)
		configFilePath = filepath.Join("./configs", configFile)
	}

	// .env 파일 로드
	if err := godotenv.Load(envFilePath); err != nil {
		return nil, fmt.Errorf(".env 파일을 찾을 수 없음: %w", err)
	}

	// yaml 파일 로드
	v := viper.New()

	v.SetConfigFile(configFilePath)

	// 환경 변수 지원 설정
	// 예: asterisk.pass 설정은 ASTERISK_PASS 환경 변수로 덮어쓸 수 있음
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// 설정 파일 읽기
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("설정 파일을 읽는 중 오류 발생: %w", err)
	}

	// [비즈니스 로직] 설정 값 내부의 환경 변수 치환
	// YAML 파일 내에 "${AMI_PASSWORD}"와 같이 저장된 문자열을 실제 OS 환경 변수 값으로 변경합니다.
	for _, key := range v.AllKeys() {
		val := v.Get(key)
		if s, ok := val.(string); ok {
			if strings.Contains(s, "${") {
				v.Set(key, os.ExpandEnv(s))
			}
		}
	}

	// 최종적으로 설정 객체에 매핑
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("설정 데이터를 구조체로 변환하는 중 오류 발생: %w", err)
	}

	return &cfg, nil
}
