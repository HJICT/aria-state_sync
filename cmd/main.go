package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	ami "common_lib/asterisk"
	"common_lib/logger"
	"common_lib/redis"
	"statesync/internal/config"
	"statesync/internal/pidfile"
	"statesync/internal/service"
	"statesync/internal/version"

	"github.com/spf13/pflag"
	"go.uber.org/zap"
)

func main() {

	// Command line parsing
	showVersion := pflag.BoolP("version", "v", false, "Show version information and exit")
	appEnv := pflag.StringP("env", "e", "dev", "run environment (dev / prod)")
	configPath := pflag.StringP("config", "c", "config/", "Configuration file path (e.g., .env or configs/.env)")
	pflag.Parse()

	if *showVersion {
		fmt.Println(version.Info())
		os.Exit(0)
	}

	// [설정 로드] YAML 설정 파일 읽기
	cfg, err := config.LoadConfig(*appEnv, *configPath)
	if err != nil {
		// 로거 초기화 전이므로 표준 log 사용
		panic(err)
	}

	// [로거 초기화]
	// 설정 로드 직후 최우선으로 로거를 초기화합니다.
	// 경로만 지정된 경우 기본 파일명(state_sync.log), 파일명까지 지정된 경우 지정된 파일명을 적용합니다.
	// 지정되지 않았거나 권한이 없는 경우 Working directory 하위 log에 저장합니다.
	logDir, logFile := config.ResolveLogPathAndFile(cfg.Log.Path)
	logCfg := &logger.LogConfig{
		Level:      cfg.Log.Level,
		Console:    cfg.Log.Console,
		Path:       logDir,
		Filename:   logFile,
		Rotation:   cfg.Log.Rotation,
		MaxSize:    cfg.Log.MaxSize,
		MaxBackups: cfg.Log.MaxBackups,
		MaxAge:     cfg.Log.MaxAge,
		Compress:   cfg.Log.Compress,
	}
	if err := logger.Init(*appEnv, logCfg); err != nil {
		panic(err)
	}
	defer logger.Sync()

	zap.S().Infof("=== %s ===", version.Info())
	zap.S().Infof("로그 저장 파일: %s/%s", logDir, logFile)

	// [PID 파일 저장]
	// 설정 파일의 pid_file 위치를 참조하며, 미지정 또는 권한 부족 시 Working directory 하위 run 디렉토리에 저장합니다.
	pidPath, err := pidfile.WritePID(cfg.PidFile)
	if err != nil {
		zap.S().Fatalf("PID 파일 생성 실패: %v", err)
	}
	zap.S().Infof("PID 파일 생성 완료: %s (PID: %d)", pidPath, os.Getpid())
	defer func() {
		if err := pidfile.RemovePID(pidPath); err != nil {
			zap.S().Warnf("PID 파일 삭제 실패: %v", err)
		} else {
			zap.S().Infof("PID 파일 삭제 완료: %s", pidPath)
		}
	}()

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
	zap.S().Infof("동기화 주기:   %s", cfg.Sync.GetInterval())

	// [Asterisk AMI 연결 설정]
	amiClient := ami.New(ctx, zap.S(), cfg.Asterisk.Host, cfg.Asterisk.Port, cfg.Asterisk.User, cfg.Asterisk.Pass)

	// [서비스 초기화 및 시작]
	// Asterisk와 Redis를 연결하여 상태를 동기화하는 비즈니스 로직 서비스를 초기화합니다.
	syncService := service.NewSyncService(amiClient, redisClient, cfg.Sync.GetInterval())
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
