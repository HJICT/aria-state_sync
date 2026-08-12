package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	ami "common_lib/asterisk"
	"common_lib/logger"
	"common_lib/redis"
	"statesync/internal/config"
	"statesync/internal/service"

	"github.com/spf13/pflag"
	"go.uber.org/zap"
)

func main() {

	// 커멘드라인 파싱
	appEnv := pflag.StringP("env", "e", "dev", "run environment (dev / prod)")
	configPath := pflag.StringP("config", "c", "", "Configuration file path (e.g., .env or configs/.env)")
	pflag.Parse()

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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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

	// SIGINT/SIGTERM 수신 시까지 대기 후 정상 종료(defer된 리소스 정리 수행)
	<-ctx.Done()
	zap.S().Info("종료 신호를 수신했습니다. 정상 종료를 시작합니다.")
}
