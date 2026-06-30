package main

import (
	"context"
	"flag"
	"os"

	ami "common_lib/asterisk"
	"common_lib/logger"
	"common_lib/redis"
	"statesync/internal/config"
	"statesync/internal/service"

	"go.uber.org/zap"
)

func main() {

	// 커멘드라인 파싱
	appEnv := flag.String("env", "dev", "run environment (dev / prod)")
	configPath := flag.String("config", "", "Configuration file path (e.g., .env or configs/.env)")
	flag.Parse()

	// [설정 로드] YAML 설정 파일 읽기
	cfg, err := config.LoadConfig(*appEnv, configPath)
	if err != nil {
		// 로거 초기화 전이므로 표준 log 사용
		panic(err)
	}

	// [로거 초기화]
	// 설정 로드 직후 최우선으로 로거를 초기화합니다.
	logCfg := &logger.LogConfig{
		Level:      cfg.Logs.Level,
		Path:       cfg.Logs.Path,
		Rotation:   cfg.Logs.Rotation,
		MaxSize:    cfg.Logs.MaxSize,
		MaxBackups: cfg.Logs.MaxBackups,
		MaxAge:     cfg.Logs.MaxAge,
		Compress:   cfg.Logs.Compress,
	}
	if err := logger.Init(*appEnv, logCfg); err != nil {
		panic(err)
	}
	defer logger.Sync()

	// [Redis 초기화]
	ctx := context.Background()
	redisClient, err := redis.NewClient(ctx, zap.S(), cfg.Redis.Host, cfg.Redis.Port, cfg.Redis.Pass)
	if err != nil {
		zap.S().Fatalf("Redis 초기화 실패: %v", err)
	}
	defer redisClient.Close()

	// [검증 및 로그 출력] 로드된 설정 확인
	zap.S().Info("=== 애플리케이션 설정 로드 완료 ===")
	zap.S().Infof("Asterisk 서버: %s:%d", cfg.Asterisk.Host, cfg.Asterisk.Port)
	zap.S().Infof("Asterisk 계정: %s", cfg.Asterisk.User)
	zap.S().Infof("Redis 서버:    %s:%d", cfg.Redis.Host, cfg.Redis.Port)

	// 환경 변수 치환 여부 확인 (AMI 비밀번호가 성공적으로 로드되었는지 검증)
	if cfg.Asterisk.Pass != "" && !isValidExpansion(os.ExpandEnv("${AMI_PASSWORD}"), cfg.Asterisk.Pass) {
		zap.S().Warn("Asterisk 비밀번호가 환경 변수에서 올바르게 치환되지 않았을 수 있습니다.")
	} else if cfg.Asterisk.Pass != "" {
		zap.S().Debug("Asterisk 비밀번호 검증 완료")
	}

	// [Asterisk AMI 연결 설정]
	amiClient := ami.New(ctx, zap.S(), cfg.Asterisk.Host, cfg.Asterisk.Port, cfg.Asterisk.User, cfg.Asterisk.Pass)

	// [서비스 초기화 및 시작]
	// Asterisk와 Redis를 연결하여 상태를 동기화하는 비즈니스 로직 서비스를 초기화합니다.
	syncService := service.NewSyncService(amiClient, redisClient)
	syncService.Start(ctx)

	// 프로그램 종료 시 연결을 안전하게 닫습니다.
	defer amiClient.Close()

	zap.S().Info("=== Asterisk AMI 연결 시도 중 ===")
	if err := amiClient.Connect(); err != nil {
		zap.S().Fatalf("Asterisk AMI 연결 오류: %v", err)
	}

	zap.S().Info("모든 초기화가 완료되었습니다. 서버가 가동 중입니다.")

	// 서버가 계속 실행되도록 대기 (향후 시그널 처리 등으로 대체 가능)
	select {}
}

// isValidExpansion 치환된 값과 실제 값이 일치하는지 확인하는 보조 함수입니다.
func isValidExpansion(expanded, actual string) bool {
	return expanded == actual
}
