# 프로젝트 변수 설정
BUILD_DIR=bin
BINARY_NAME=statesync-server
MAIN_FILE=./cmd/server/main.go

.PHONY: all build run clean deps help

# 기본 타겟 (빌드 실행)
all: build

## build: 프로젝트를 빌드하여 실행 파일을 생성합니다.
build:
	@echo "⚙️ Building application..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_FILE)	

## clean: 빌드된 바이너리와 로그 파일을 삭제합니다.
clean:
	@echo "🪄 Cleaning..."
	rm -rf $(BUILD_DIR)	

## help: 사용 가능한 명령어 목록을 출력합니다.
help:
	@echo "사용 가능한 명령어:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'
