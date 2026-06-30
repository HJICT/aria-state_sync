# Asterisk-Redis 상태 동기화 서비스 (StateSync) 연동 개발 가이드

본 문서는 `StateSync` 서비스가 Asterisk PBX로부터 수집하여 Redis에 저장 및 발행하는 실시간 상태 데이터를 타 서비스(Web Dashboard, 모니터링 시스템, 통계 서버 등)에서 연동하여 사용하기 위한 개발 가이드입니다.

---

## 📌 개요

`StateSync` 서비스는 Asterisk의 AMI 이벤트를 실시간으로 구독하여 내선의 **통화 상태(Device State)** 및 **네트워크 가동 상태(Reachable State)**를 표준화된 형태로 가공하여 Redis에 제공합니다.

연동 시스템은 Redis를 통해 두 가지 방식으로 데이터를 연동할 수 있습니다.
1. **일괄/개별 조회 (Pull)**: Redis Hash 자료구조를 조회하여 특정 내선 혹은 전체 내선의 현재 상태를 즉시 가져옵니다.
2. **실시간 변경 감지 (Push)**: Redis Pub/Sub 채널을 구독하여 상태가 변경되는 시점에 실시간 이벤트를 수신합니다.

---

## 🗄️ 1. Redis 데이터 구조 (조회 연동)

각 내선의 최신 상태는 Redis Hash에 실시간으로 업데이트됩니다.

### 1.1 기기 통화 상태 (Device State)

내선의 현재 통화 동작 상태 및 통화 상대방 정보를 조회합니다.

* **Key 포맷**: `asterisk:exten:{extension}:device_state`
* **자료구조**: Hash
* **필드 목록**:

| 필드명 | 데이터 타입 | 설명 | 예시 값 |
| :--- | :--- | :--- | :--- |
| `state` | String | 내선의 통화 상태 (아래 상태 정의 참조) | `Busy`, `Idle`, `Ringing` |
| `call_class` | String | 통화 분류 (`Normal`: 일반 통화, `Broadcast`: 방송 통화) | `Normal`, `Broadcast` |
| `connected_line_num` | String | 연결된 상대방 내선번호 (없을 경우 빈 값) | `1002` |
| `connected_line_name` | String | 연결된 상대방의 이름/정보 (없을 경우 빈 값) | `Broadcasting`, `1002` |

* **`connected_line_name`** 에 "Broadcasting" 이 포함된 경우는 방송 통화로 인식되어 `call_class`가 `Broadcast`로 설정됩니다.

> **Redis CLI 조회 예시**:
> ```bash
> HGETALL asterisk:exten:1001:device_state
> # 출력 결과:
> # 1) "state"
> # 2) "Busy"
> # 3) "call_class"
> # 4) "Normal"
> # 5) "connected_line_num"
> # 6) "1002"
> # 7) "connected_line_name"
> # 8) "Hong Gil Dong"
> ```

### 1.2 네트워크 상태 (Reachability State)

내선 단말의 네트워크 연결 상태를 조회합니다.

* **Key 포맷**: `asterisk:exten:{extension}:reachability`
* **자료구조**: Hash
* **필드 목록**:

| 필드명 | 데이터 타입 | 설명 | 예시 값 |
| :--- | :--- | :--- | :--- |
| `state` | String | 네트워크 가동 상태 (아래 상태 정의 참조) | `Reachable`, `Unreachable` |

> **Redis CLI 조회 예시**:
> ```bash
> HGETALL asterisk:exten:1001:reachability
> # 출력 결과:
> # 1) "state"
> # 2) "Reachable"
> ```

---

## 🔔 2. Redis Pub/Sub (실시간 이벤트 연동)

상태 변화가 발생할 때마다 아래의 전용 Redis 채널로 JSON 포맷의 이벤트가 실시간 발행됩니다.

### 2.1 통화 상태 변경 이벤트 채널

* **Channel**: `asterisk:device_state`
* **Payload 포맷**: JSON
* **필드 상세**:

| 필드명 | 데이터 타입 | 설명 |
| :--- | :--- | :--- |
| `event_type` | String | 이벤트 고유 타입 (항상 `DeviceState`) |
| `class` | String | 통화 분류 (`Normal`: 일반 통화, `Broadcast`: 방송 통화) |
| `exten` | String | 상태가 변경된 대상 내선번호 |
| `state` | String | 변경된 통화 상태값 (아래 상태 정의 참조) |
| `connected_line_num` | String | 연결된 상대방 내선번호 (통화 중이 아닐 경우 빈 값) |
| `connected_line_name` | String | 연결된 상대방의 이름/정보 (통화 중이 아닐 경우 빈 값) |
| `timestamp` | String | 이벤트 발생 시간 (RFC3339 포맷, KST 반영) |

* **`connected_line_name`** 에 "Broadcasting" 이 포함된 경우는 방송 통화로 인식되어 `call_class`가 `Broadcast`로 설정됩니다.

> **JSON 페이로드 예시 (일반 통화 연결)**:
> ```json
> {
>   "event_type": "DeviceState",
>   "class": "Normal",
>   "exten": "1001",
>   "state": "Busy",
>   "connected_line_num": "1002",
>   "connected_line_name": "Hong Gil Dong",
>   "timestamp": "2026-06-30T09:55:00+09:00"
> }
> ```

> **JSON 페이로드 예시 (방송 송출)**:
> ```json
> {
>   "event_type": "DeviceState",
>   "class": "Broadcast",
>   "exten": "1005",
>   "state": "Use",
>   "connected_line_num": "9999",
>   "connected_line_name": "Broadcasting-Zone1",
>   "timestamp": "2026-06-30T09:56:00+09:00"
> }
> ```

### 2.2 네트워크 상태 변경 이벤트 채널

* **Channel**: `asterisk:reachable_state`
* **Payload 포맷**: JSON
* **필드 상세**:

| 필드명 | 데이터 타입 | 설명 |
| :--- | :--- | :--- |
| `event_type` | String | 이벤트 고유 타입 (항상 `ReachableState`) |
| `exten` | String | 네트워크 상태가 변경된 대상 내선번호 |
| `state` | String | 변경된 네트워크 상태값 (아래 상태 정의 참조) |
| `timestamp` | String | 이벤트 발생 시간 (RFC3339 포맷, KST 반영) |

> **JSON 페이로드 예시 (네트워크 단절)**:
> ```json
> {
>   "event_type": "ReachableState",
>   "exten": "1001",
>   "state": "Unreachable",
>   "timestamp": "2026-06-30T09:55:10+09:00"
> }
> ```

---

## 🚥 3. 상태값 정의 (State Definitions)

### 3.1 Device State (통화 상태)

단말 기기의 통화 처리 상태를 나타냅니다.

| 상태값 | 설명 | 매핑된 AMI 이벤트 상태 |
| :--- | :--- | :--- |
| `Idle` | 통화 중이 아니며 대기 상태 (통화 가능) | Not in use, Hangup 이벤트 수신 시 |
| `Use` | 통화 연결되어 사용 중 | In use, Up (Channel State 6) |
| `Busy` | 통화 중 (통화 불가능) | Busy (Channel State 7) |
| `Ringing` | 벨이 울리는 중 (발신/수신 호출 진행 중) | Ringing, Ring/Ringing (Channel State 4, 5) |
| `RingUse` | 통화 중이면서 새로운 호의 벨이 울림 | Ring+Inuse |
| `Hold` | 통화 보류 중 | On Hold |
| `Unavailable` | 기기 사용 불가 (오프라인 상태) | Contact Status가 Unreachable인 경우 자동 전이 |
| `Unknown` | 상태를 알 수 없음 | Unknown, Invalid |

### 3.2 Reachable State (네트워크 상태)

단말 기기와 Asterisk PBX 간의 네트워크 연결 상태를 나타냅니다.

| 상태값 | 설명 | 매핑된 AMI Contact Status |
| :--- | :--- | :--- |
| `Reachable` | 네트워크 연결 양호 (가동 상태) | Reachable, Updated |
| `Unreachable` | 네트워크 연결 단절 (장비 전원 오프 등) | Unreachable, Removed |
| `Unknown` | 네트워크 연결 상태 확인 불가 | Unknown, Unqualified |

### 3.3 Call Class (통화 종류)

| 분류값 | 설명 | 판별 조건 |
| :--- | :--- | :--- |
| `Normal` | 일반 내선 간 통화 혹은 외부 통화 | 기본값 |
| `Broadcast` | 방송 설비를 이용한 강제 방송/안내 통화 | 상대방 이름(`connected_line_name`)에 "Broadcasting" 포함 시 |

---

## 💻 4. 연동 예제 코드

### 4.1 Node.js (ioRedis 사용)

```javascript
const Redis = require('ioredis');
const redis = new Redis({
  host: '127.0.0.1',
  port: 6379,
  // password: 'your_password' // 필요한 경우 설정
});

// 1. 특정 내선 상태 개별 조회 함수
async function getExtensionState(exten) {
  const deviceState = await redis.hgetall(`asterisk:exten:${exten}:device_state`);
  const reachability = await redis.hgetall(`asterisk:exten:${exten}:reachability`);
  
  console.log(`--- [내선 ${exten}] 현재 상태 ---`);
  console.log(`통화 상태: ${deviceState.state || 'Unknown'}`);
  console.log(`통화 분류: ${deviceState.call_class || 'Normal'}`);
  console.log(`연결 번호: ${deviceState.connected_line_num || 'None'}`);
  console.log(`상대 이름: ${deviceState.connected_line_name || 'None'}`);
  console.log(`네트 상태: ${reachability.state || 'Unknown'}`);
}

// 2. 실시간 이벤트 구독 시작
const pubsub = new Redis({ host: '127.0.0.1', port: 6379 });
pubsub.subscribe('asterisk:device_state', 'asterisk:reachable_state', (err, count) => {
  if (err) {
    console.error('구독 실패:', err);
    return;
  }
  console.log(`성공적으로 ${count}개의 채널을 구독했습니다.`);
});

pubsub.on('message', (channel, message) => {
  const event = JSON.parse(message);
  console.log(`\n📢 [이벤트 수신 - 채널: ${channel}]`);
  console.log(JSON.stringify(event, null, 2));
});

// 테스트 실행
setTimeout(() => getExtensionState('1001'), 1000);
```

### 4.2 Python (redis-py 사용)

```python
import redis
import json
import threading

# Redis 클라이언트 생성
r = redis.Redis(host='127.0.0.1', port=6379, decode_responses=True)

# 1. 내선 상태 조회
def get_extension_state(exten):
    device_state = r.hgetall(f"asterisk:exten:{exten}:device_state")
    reachability = r.hgetall(f"asterisk:exten:{exten}:reachability")
    
    print(f"--- [내선 {exten}] 현재 상태 ---")
    print(f"통화 상태: {device_state.get('state', 'Unknown')}")
    print(f"통화 분류: {device_state.get('call_class', 'Normal')}")
    print(f"연결 번호: {device_state.get('connected_line_num', 'None')}")
    print(f"상대 이름: {device_state.get('connected_line_name', 'None')}")
    print(f"네트 상태: {reachability.get('state', 'Unknown')}\n")

# 2. 실시간 이벤트 구독 핸들러
def start_subscriber():
    p = r.pubsub()
    p.subscribe('asterisk:device_state', 'asterisk:reachable_state')
    print("실시간 이벤트 구독을 시작합니다...")
    
    for message in p.listen():
        if message['type'] == 'message':
            channel = message['channel']
            data = json.loads(message['data'])
            print(f"\n📢 [이벤트 수신 - {channel}]")
            print(json.dumps(data, indent=2, ensure_ascii=False))

# 구독 스레드 구동
sub_thread = threading.Thread(target=start_subscriber, daemon=True)
sub_thread.start()

# 상태 조회 호출 테스트
import time
time.sleep(1)
get_extension_state("1001")

# 메인 프로세스 유지
try:
    while True:
        time.sleep(1)
except KeyboardInterrupt:
    print("종료합니다.")
```
