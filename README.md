# StateSync (Asterisk-Redis State Synchronizer)

`StateSync`는 Asterisk PBX의 AMI(Asterisk Manager Interface) 이벤트를 실시간으로 구독하여, 내선(Extension/Endpoint)의 통화 상태 및 네트워크 가동 상태를 Redis에 동기화하는 Go 기반 서비스입니다.

본 프로젝트는 공통 모듈인 `common_lib`을 참조하여 AMI 클라이언트, Redis 클라이언트 및 로깅 기능을 공유합니다.

## 🚀 프로젝트 개요

이 서비스는 Asterisk 서버의 상태 변화를 실시간으로 추적하고, 이를 중앙 집중식 데이터 저장소인 Redis에 반영합니다. 이를 통해 다른 애플리케이션(Web Dashboard, 통계 서버 등)이 Asterisk에 직접 쿼리하지 않고도 최신 내선 상태를 쉽고 빠르게 조회할 수 있도록 돕습니다.

## ✨ 주요 기능

-   **Asterisk AMI 실시간 이벤트 구독**: `ContactStatus`, `Newstate`, `Hangup`, `EndpointList` 등 주요 이벤트를 수신하고 처리합니다.
-   **내선 상태 추적**:
    -   **DeviceState**: 통화 중(BUSY), 벨 울림(RINGING), 통화 가능(IDLE) 등의 전화기 사용 상태.
    -   **ReachableState**: 네트워크 연결됨(REACHABLE), 연결 끊김(UNREACHABLE) 등의 네트워크 상태.
-   **Redis 동기화**:
    -   **Hash Storage**: 각 내선별 최신 상태를 Redis Hash에 실시간 업데이트.
    -   **Pub/Sub Notification**: 상태 변경 시 전용 채널로 실시간 JSON 이벤트 발행.
-   **안정성**:
    -   **자동 재접속**: Asterisk AMI 또는 Redis 연결 유실 시 자동 복구 메커니즘 작동.
    -   **초기 상태 동기화**: 서비스 시작 또는 재연결 시 `PJSIPShowEndpoints`를 통해 전체 상태를 즉시 갱신.
-   **로깅 및 모니터링**: `zap` 로거를 통한 상세 로그 기록 및 `lumberjack`을 이용한 로그 로테이션 지원.

## 📋 요구 사항

-   **Go**: 1.26.3 이상
-   **Asterisk**: 16+ (PJSIP 사용 권장, AMI 활성화 필요)
-   **Redis**: 7.0+ (v9 호환)
-   **Common Library**: `common_lib` (상위 디렉토리에 위치해야 함)

## 🛠️ 설치 및 실행

### 1. 프로젝트 구조 확인

본 프로젝트는 상위 디렉토리의 `common_lib`을 참조합니다. 다음과 같은 디렉토리 구조를 권장합니다.

```text
/ARIA
├── common_lib/
└── state_sync/
```

### 2. 설정 파일 준비

`configs/config.yaml.example` 파일을 복사하여 실제 환경에 맞는 설정 파일을 생성합니다.

```bash
cp configs/config.yaml.example configs/config.dev.yaml
```

`.env.dev` 파일을 생성하여 민감한 정보(비밀번호 등)를 설정할 수 있습니다.

### 2. 빌드 및 실행

```bash
# 의존성 설치
go mod download

# 서버 실행 (개발 환경)
go run cmd/server/main.go -env dev

# 또는 빌드 후 실행
go build -o statesync-server cmd/server/main.go
./statesync-server -env dev
```

## ⚙️ 설정 (Configuration)

설정은 `configs/config.{env}.yaml` 파일에서 관리하며, 환경 변수 치환(`${VARIABLE_NAME}`)을 지원합니다.

-   **Asterisk**: AMI 서버 호스트, 포트, 사용자 계정, 비밀번호.
-   **Redis**: Redis 서버 주소, 포트, 인증 비밀번호.
-   **Logs**: 로그 레벨, 저장 경로, 로테이션 설정(Max Size, Backups 등).

## 🗄️ Redis 데이터 구조

### 1. Hash Keys (상태 저장)

-   **기기 통화 상태**
    -   Key: `asterisk:exten:{extension}:device_state`
    -   Fields: `state` (상태값), `connected_line` (상대방 번호)
-   **네트워크 연결 상태**
    -   Key: `asterisk:exten:{extension}:reachability`
    -   Fields: `state` (상태값)

### 2. Pub/Sub Channels (실시간 알림)

-   **Channel**: `asterisk:device_state`
    -   Payload (JSON): `{"event_type": "DeviceState", "class": "Normal", "exten": "1001", "state": "Busy", "connected_line": "1002", "timestamp": "..."}`
-   **Channel**: `asterisk:reachable_state`
    -   Payload (JSON): `{"event_type": "ReachableState", "exten": "1001", "state": "Reachable", "timestamp": "..."}`

## 🚥 상태 정의 (State Definitions)

### Call Class (통화 종류)
- `Normal`: 일반 통화
- `Broadcast`: 방송용 통화

### Device State (통화 상태)
-   `Idle`: 대기 상태
-   `Use`: 통화 중
-   `Busy`: 통화 중 (바쁨)
-   `Ringing`: 벨 울림
-   `Ring-Use`: 통화 중 + 벨 울림 (멀티 라인)
-   `Hold`: 통화 보류
-   `Unavailable`: 기기 사용 불가
-   `Unknown`: 알 수 없음

### Reachable State (네트워크 상태)
-   `Reachable`: 네트워크 연결 가능 (가동 중)
-   `Unreachable`: 네트워크 연결 끊김 (중단)
-   `Unknown`: 상태 확인 불가
