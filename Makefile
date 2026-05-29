# 프로젝트 변수 설정
BINARY_NAME=statesync-server
MAIN_FILE=cmd/server/main.go

.PHONY: all build run clean deps help

# 기본 타겟 (빌드 실행)
all: build

## build: 프로젝트를 빌드하여 실행 파일을 생성합니다.
build:
	@echo "빌드 시작..."
	go build -o $(BINARY_NAME) $(MAIN_FILE)
	@echo "빌드 완료: $(BINARY_NAME)"

## run: 애플리케이션을 즉시 실행합니다. (개발용)
run:
	@echo "애플리케이션 실행 중..."
	go run $(MAIN_FILE) -env dev

## clean: 빌드된 바이너리와 로그 파일을 삭제합니다.
clean:
	@echo "정리 중..."
	rm -f $(BINARY_NAME)
	rm -rf logs/*
	@echo "정리 완료."

## deps: 의존성을 정리하고 다운로드합니다.
deps:
	@echo "의존성 정리 중..."
	go mod tidy
	go mod download
	@echo "의존성 정리 완료."

## help: 사용 가능한 명령어 목록을 출력합니다.
help:
	@echo "사용 가능한 명령어:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'
