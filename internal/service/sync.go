package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	ami "common_lib/asterisk"
	"common_lib/logger"
	"common_lib/redis"

	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// DeviceState 는 시스템 내부에서 관리하는 장치 상태 표준 타입입니다.
type DeviceState string

const (
	DeviceStateIdle        DeviceState = "Idle"
	DeviceStateUse         DeviceState = "Use"
	DeviceStateBusy        DeviceState = "Busy"
	DeviceStateRinging     DeviceState = "Ringing"
	DeviceStateRingUse     DeviceState = "RingUse"
	DeviceStateHold        DeviceState = "Hold"
	DeviceStateUnavailable DeviceState = "Unavailable"
	DeviceStateUnknown     DeviceState = "Unknown"
)

// ReachableState 는 시스템 내부에서 관리하는 네트워크(장치) 상태 표준 타입입니다.
type ReachableState string

const (
	ReachableStateAvailable   ReachableState = "Reachable"
	ReachableStateUnavailable ReachableState = "Unreachable"
	ReachableStateUnknown     ReachableState = "Unknown"
)

// Asterisk AMI ChannelState 정수값 (https://docs.asterisk.org/Asterisk_20_Documentation/API_Documentation/AMI_Events/Newstate/)
const (
	chanStateRing    = 4 // Ring
	chanStateRinging = 5 // Ringing
	chanStateUp      = 6 // Up
	chanStateBusy    = 7 // Busy
)

// StateChangeEvent 는 Redis Pub/Sub을 통해 전송되는 상태 변경 이벤트 데이터 구조입니다.
type DeviceStateChangeEvent struct {
	EventType         string `json:"event_type"`
	CallClass         string `json:"class"`
	Exten             string `json:"exten"`
	State             string `json:"state"`
	ConnectedLineNum  string `json:"connected_line_num"`
	ConnectedLineName string `json:"connected_line_name"`
	Timestamp         string `json:"timestamp"`
}

type ReachableStateChangeEvent struct {
	EventType string `json:"event_type"`
	Exten     string `json:"exten"`
	State     string `json:"state"`
	IPAddress string `json:"ip_address"`
	UserAgent string `json:"user_agent"`
	Timestamp string `json:"timestamp"`
}

// ContactItem 은 ContactList 이벤트 버퍼링을 위한 데이터 구조입니다.
type ContactItem struct {
	Endpoint  string
	UserAgent string
	IPAddress string
}

// updateContactScript 는 Redis에 대상 키가 존재하는 경우에만 UserAgent와 IPAddress를 업데이트하는 Lua 스크립트입니다.
var updateContactScript = goredis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 1 then
    if ARGV[2] ~= '' then
        redis.call('HSET', KEYS[1], 'user_agent', ARGV[1], 'ip_address', ARGV[2])
    else
        redis.call('HSET', KEYS[1], 'user_agent', ARGV[1])
    end
    return 1
end
return 0
`)

// EndpointData 는 Redis 업데이트 전 버퍼링을 위한 데이터 구조입니다.
type EndpointData struct {
	DeviceState    DeviceState
	ReachableState ReachableState
	IPAddress      string
	UserAgent      string
}

// SyncService Asterisk의 상태 정보를 Redis로 동기화하는 비즈니스 로직을 담당합니다.
type SyncService struct {
	ami          *ami.AmiClient
	redis        *redis.RedisClient
	syncInterval time.Duration

	bufferMu          sync.Mutex
	endpointBuffer    map[string]EndpointData
	bufferTimer       *time.Timer
	expectedItems     int
	contactsRequested bool

	contactSyncTimer   *time.Timer
	contactMu          sync.Mutex
	contactBuffer      map[string]ContactItem
	contactBufferTimer *time.Timer
	contactExpected    int
}

// NewSyncService 새로운 SyncService 인스턴스를 생성합니다.
func NewSyncService(ami *ami.AmiClient, redis *redis.RedisClient, syncInterval time.Duration) *SyncService {
	if syncInterval <= 0 {
		syncInterval = time.Hour
	}
	return &SyncService{
		ami:            ami,
		redis:          redis,
		syncInterval:   syncInterval,
		endpointBuffer: make(map[string]EndpointData),
		contactBuffer:  make(map[string]ContactItem),
	}
}

// Start AMI 이벤트를 구독하고 Redis 동기화 로직을 실행합니다.
func (s *SyncService) Start(ctx context.Context) {
	zap.S().Info("[SyncService] Asterisk-Redis 상태 동기화 서비스를 시작합니다.")

	// EndpointList 이벤트 처리 (초기 및 주기 상태 정보 조회)
	s.ami.SubscribeEvent("EndpointList", func(msg ami.Message) {
		s.handlerEndpointList(ctx, msg)
	})

	// EndpointListComplete 이벤트 처리 (Endpoint 상태 정보 조회 완료)
	s.ami.SubscribeEvent("EndpointListComplete", func(msg ami.Message) {
		s.handlerEndpointListComplete(ctx, msg)
	})

	// ContactList 이벤트 처리 (Contact 및 UserAgent 정보 수신)
	s.ami.SubscribeEvent("ContactList", func(msg ami.Message) {
		s.handlerContactList(ctx, msg)
	})

	// ContactListComplete 이벤트 처리 (Contact 정보 조회 완료)
	s.ami.SubscribeEvent("ContactListComplete", func(msg ami.Message) {
		s.handlerContactListComplete(ctx, msg)
	})

	// ContactStatus 이벤트 처리 (장치 상태 변경 감지)
	s.ami.SubscribeEvent("ContactStatus", func(msg ami.Message) {
		s.handlerContactStateChange(ctx, msg)
	})

	// Newstate 이벤트 처리 (통화 상태 변경 감지)
	s.ami.SubscribeEvent("Newstate", func(msg ami.Message) {
		s.handlerNewStateChange(ctx, msg)
	})

	// Hangup 이벤트 처리 (통화 종료 감지)
	s.ami.SubscribeEvent("Hangup", func(msg ami.Message) {
		s.handlerHangup(ctx, msg)
	})

	// AMI 재연결 시 초기 동기화 처리
	s.ami.SubscribeEvent("_ClientConnected", func(msg ami.Message) {
		zap.S().Info("[SyncService] AMI 연결됨. 전체 상태 동기화를 시작합니다.")
		s.triggerFullSync(ctx)
	})

	// 주기적 전체 동기화 타이머 고루틴 시작
	go s.startPeriodicSync(ctx)
}

// startPeriodicSync 설정된 주기마다 전체 동기화 및 삭제 단말 정리를 실행합니다.
func (s *SyncService) startPeriodicSync(ctx context.Context) {
	ticker := time.NewTicker(s.syncInterval)
	defer ticker.Stop()

	zap.S().Infof("[SyncService] 주기 동기화 타이머를 시작합니다. (주기: %v)", s.syncInterval)

	for {
		select {
		case <-ctx.Done():
			zap.S().Info("[SyncService] 주기 동기화 고루틴을 종료합니다.")
			return
		case <-ticker.C:
			zap.S().Infof("[SyncService] 동기화 주기(%v) 도달. 전체 동기화 및 삭제 단말 정리를 실행합니다.", s.syncInterval)
			s.triggerFullSync(ctx)
		}
	}
}

// triggerFullSync 전체 상태 동기화 요청(PJSIPShowEndpoints)을 전송합니다.
func (s *SyncService) triggerFullSync(ctx context.Context) {
	s.bufferMu.Lock()
	s.contactsRequested = false
	s.bufferMu.Unlock()

	zap.S().Info("[SyncService] 전체 동기화 요청(PJSIPShowEndpoints)을 전송합니다.")
	_, err := s.ami.Action(ami.Message{
		"Action": "PJSIPShowEndpoints",
	})
	if err != nil {
		zap.S().Errorf("[SyncService] PJSIPShowEndpoints 요청 실패: %v", err)
	}
}

// handlerEndpointList 장치의 초기 상태 정보 이벤트를 처리 합니다.
func (s *SyncService) handlerEndpointList(ctx context.Context, msg ami.Message) {
	device := msg["ObjectName"]
	state := msg["DeviceState"]
	conacts := msg["Contacts"]

	if device == "" || state == "" {
		zap.S().Warnf("[EndPointList] 이벤트에 사용 가능한 데이터가 없습니다. (ObjectName=%q, DeviceState=%q)", device, state)
		return
	}

	// IP주소 조회
	ipaddr := extractSipIPv4(conacts)

	// DeviceState 문자열을 표준 DeviceState 변환
	deviceState := parseDeviceState(state)

	// DeviceState의 상태값이 'DeviceStateUnavailable'이면 reachableState를 'ReachableStateUnavailable' 으로 설정
	reachableState := ReachableStateAvailable
	if deviceState == DeviceStateUnavailable {
		reachableState = ReachableStateUnavailable
	}

	s.bufferMu.Lock()
	// 버퍼에 데이터 저장
	data := s.endpointBuffer[device]
	data.DeviceState = deviceState
	data.ReachableState = reachableState
	if ipaddr != "" {
		data.IPAddress = ipaddr
	}
	s.endpointBuffer[device] = data
	bufLen, expected := len(s.endpointBuffer), s.expectedItems

	var shouldFlush bool
	if s.expectedItems > 0 && len(s.endpointBuffer) >= s.expectedItems {
		shouldFlush = true
	} else if s.bufferTimer == nil {
		// 첫 데이터 수신 시 5초 타이머 시작 (이벤트 누락 대비)
		s.bufferTimer = time.AfterFunc(5*time.Second, func() {
			zap.S().Warn("[SyncService] EndpointList 타임아웃(5초). 수신된 데이터까지만 업데이트합니다.")
			s.flushEndpointBuffer(ctx)
		})
	}
	s.bufferMu.Unlock()

	zap.S().Debugf("[EndPointList] 버퍼 저장: %s -> DeviceState=%s, Reachable=%s, IP=%s (버퍼: %d, 예상: %d)", device, deviceState, reachableState, ipaddr, bufLen, expected)

	if shouldFlush {
		s.flushEndpointBuffer(ctx)
	}
}

// handlerEndpointListComplete 장치 초기 상태 조회 완료 이벤트를 처리합니다.
func (s *SyncService) handlerEndpointListComplete(ctx context.Context, msg ami.Message) {

	listItemsStr := msg["ListItems"]
	zap.S().Debugf("[EndPointListComplete] 완료 이벤트 수신 (ListItems: %q)", listItemsStr)
	listItems, err := strconv.Atoi(listItemsStr)
	if err != nil {
		zap.S().Warnf("[EndPointListComplete] ListItems 파싱 실패: %v. 즉시 업데이트합니다.", err)
		s.flushEndpointBuffer(ctx)
		return
	}

	s.bufferMu.Lock()
	s.expectedItems = listItems
	shouldFlush := len(s.endpointBuffer) >= s.expectedItems
	currentCount := len(s.endpointBuffer)
	s.bufferMu.Unlock()

	if shouldFlush {
		s.flushEndpointBuffer(ctx)
	} else {
		zap.S().Debugf("[EndPointListComplete] 추가 데이터 대기 중 (예상: %d, 현재: %d)", listItems, currentCount)
	}
}

// flushEndpointBuffer EndpointList에서 조회된 단말 기준으로 Redis를 Pipeline으로 일괄 업데이트하고, 삭제된 단말을 제거한 뒤 ContactList 조회를 요청합니다.
func (s *SyncService) flushEndpointBuffer(ctx context.Context) {

	s.bufferMu.Lock()

	buffer := s.endpointBuffer
	s.endpointBuffer = make(map[string]EndpointData)
	s.expectedItems = 0
	s.contactsRequested = false

	if s.bufferTimer != nil {
		s.bufferTimer.Stop()
		s.bufferTimer = nil
	}

	s.bufferMu.Unlock()

	if len(buffer) == 0 {
		zap.S().Debug("[SyncService] 단말 버퍼가 비어 있어 업데이트를 건너뜁니다.")
		return
	}

	// 1. Asterisk에서 삭제된 단말 제거 (SCAN 기반 안전한 키 정리)
	s.cleanupDeletedEndpoints(ctx, buffer)

	// 2. EndpointList에서 조회된 단말 기준으로 Redis 일괄 업데이트 (Pipeline 배치 처리)
	zap.S().Infof("[SyncService] Redis에 단말 %d개를 업데이트합니다. (Pipeline)", len(buffer))

	devices := make([]string, 0, len(buffer))
	for d := range buffer {
		devices = append(devices, d)
	}

	const batchSize = 500

	// 2-1. 기존 Redis에 저장된 UserAgent를 Pipeline으로 일괄 조회 (유실 방지)
	existingUAs := make(map[string]string, len(buffer))
	for i := 0; i < len(devices); i += batchSize {
		end := i + batchSize
		if end > len(devices) {
			end = len(devices)
		}
		chunk := devices[i:end]

		pipe := s.redis.Pipeline()
		cmds := make(map[string]*goredis.StringCmd, len(chunk))
		for _, dev := range chunk {
			reachKey := fmt.Sprintf("asterisk:exten:%s:reachability", dev)
			cmds[dev] = pipe.HGet(ctx, reachKey, "user_agent")
		}
		_, _ = pipe.Exec(ctx) // 키 미존재(redis.Nil) 에러는 무시

		for dev, cmd := range cmds {
			if val, err := cmd.Result(); err == nil && val != "" {
				existingUAs[dev] = val
			}
		}
	}

	// 2-2. DeviceState 및 Reachability 상태를 Pipeline으로 일괄 저장 및 이벤트 발행
	nowKST := time.Now().Local().Format(time.RFC3339)

	for i := 0; i < len(devices); i += batchSize {
		end := i + batchSize
		if end > len(devices) {
			end = len(devices)
		}
		chunk := devices[i:end]

		pipe := s.redis.Pipeline()
		for _, dev := range chunk {
			data := buffer[dev]
			ua := data.UserAgent
			if ua == "" {
				ua = existingUAs[dev]
			}

			callClass := "Normal"

			// DeviceState HSet
			devKey := fmt.Sprintf("asterisk:exten:%s:device_state", dev)
			pipe.HSet(ctx, devKey,
				"state", string(data.DeviceState),
				"call_class", callClass,
				"connected_line_num", "",
				"connected_line_name", "")

			// DeviceState Pub/Sub
			devEvent := DeviceStateChangeEvent{
				EventType:         "DeviceState",
				CallClass:         callClass,
				Exten:             dev,
				State:             string(data.DeviceState),
				ConnectedLineNum:  "",
				ConnectedLineName: "",
				Timestamp:         nowKST,
			}
			if devPayload, err := json.Marshal(devEvent); err == nil {
				pipe.Publish(ctx, "asterisk:device_state", devPayload)
			}

			// Reachability HSet
			reachKey := fmt.Sprintf("asterisk:exten:%s:reachability", dev)
			pipe.HSet(ctx, reachKey,
				"state", string(data.ReachableState),
				"ip_address", data.IPAddress,
				"user_agent", ua)

			// Reachability Pub/Sub
			reachEvent := ReachableStateChangeEvent{
				EventType: "ReachableState",
				Exten:     dev,
				State:     string(data.ReachableState),
				IPAddress: data.IPAddress,
				UserAgent: ua,
				Timestamp: nowKST,
			}
			if reachPayload, err := json.Marshal(reachEvent); err == nil {
				pipe.Publish(ctx, "asterisk:reachable_state", reachPayload)
			}
		}

		if _, err := pipe.Exec(ctx); err != nil {
			zap.S().Errorf("[Redis] 단말 상태 Pipeline 업데이트 실패: %v", err)
		}
	}

	// 3. 이후 ContactList로 UserAgent 값을 업데이트
	s.requestContacts(ctx)
}

// cleanupDeletedEndpoints Asterisk에서 삭제된 단말을 SCAN 커서로 안전하게 감지하여 Redis에서 일괄 제거합니다.
func (s *SyncService) cleanupDeletedEndpoints(ctx context.Context, currentEndpoints map[string]EndpointData) {
	deletedExtens := make(map[string]struct{})

	// KEYS 대신 SCAN 커서를 사용하여 Redis 블로킹(Stop-the-World) 방지
	iter := s.redis.Scan(ctx, 0, "asterisk:exten:*", 500).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		parts := strings.Split(key, ":")
		if len(parts) >= 3 && parts[0] == "asterisk" && parts[1] == "exten" {
			exten := parts[2]
			if _, exists := currentEndpoints[exten]; !exists {
				deletedExtens[exten] = struct{}{}
			}
		}
	}
	if err := iter.Err(); err != nil {
		zap.S().Errorf("[Redis] 삭제 단말 정리 중 SCAN 실패: %v", err)
		return
	}

	if len(deletedExtens) == 0 {
		return
	}

	// 일괄 삭제할 키 목록 수집
	keysToDelete := make([]string, 0, len(deletedExtens)*2)
	for exten := range deletedExtens {
		zap.S().Debugf("[SyncService] 삭제된 단말 발견: exten=%s", exten)
		keysToDelete = append(keysToDelete,
			fmt.Sprintf("asterisk:exten:%s:device_state", exten),
			fmt.Sprintf("asterisk:exten:%s:reachability", exten),
		)
	}

	// 청크 단위(500개)로 분할하여 Del 일괄 삭제
	const batchSize = 500
	for i := 0; i < len(keysToDelete); i += batchSize {
		end := i + batchSize
		if end > len(keysToDelete) {
			end = len(keysToDelete)
		}
		chunk := keysToDelete[i:end]
		if err := s.redis.Del(ctx, chunk...).Err(); err != nil {
			zap.S().Errorf("[Redis] 삭제된 단말의 키 제거 실패: %v", err)
		}
	}

	zap.S().Infof("[SyncService] Redis에서 삭제된 단말 %d개(키 %d개)를 제거했습니다.", len(deletedExtens), len(keysToDelete))
}

// requestContacts Contact 및 UserAgent 조회를 위해 PJSIPShowContacts 액션을 전송합니다.
func (s *SyncService) requestContacts(ctx context.Context) {
	s.bufferMu.Lock()
	if s.contactsRequested {
		s.bufferMu.Unlock()
		return
	}
	s.contactsRequested = true
	s.bufferMu.Unlock()

	// Contact 버퍼 초기화
	s.contactMu.Lock()
	s.contactBuffer = make(map[string]ContactItem)
	s.contactExpected = 0
	if s.contactBufferTimer != nil {
		s.contactBufferTimer.Stop()
		s.contactBufferTimer = nil
	}
	s.contactMu.Unlock()

	zap.S().Info("[SyncService] UserAgent 갱신을 위해 Contact 목록(PJSIPShowContacts)을 요청합니다.")

	_, err := s.ami.Action(ami.Message{
		"Action": "PJSIPShowContacts",
	})
	if err != nil {
		zap.S().Errorf("[SyncService] PJSIPShowContacts 요청 실패: %v", err)
	}
}

// handlerContactList Contact 정보 이벤트를 버퍼에 수집합니다.
func (s *SyncService) handlerContactList(ctx context.Context, msg ami.Message) {
	endpoint := msg["Endpoint"]
	userAgent := msg["UserAgent"]
	viaAddr := msg["ViaAddr"]
	uri := msg["Uri"]

	if endpoint == "" || userAgent == "" {
		zap.S().Debugf("[ContactList] 필수 필드 누락으로 무시: Endpoint=%q, UserAgent=%q", endpoint, userAgent)
		return
	}

	ip := viaAddr
	if ip == "" && uri != "" {
		ip = extractSipIPv4(uri)
	}

	s.contactMu.Lock()
	s.contactBuffer[endpoint] = ContactItem{
		Endpoint:  endpoint,
		UserAgent: userAgent,
		IPAddress: ip,
	}
	bufLen, expected := len(s.contactBuffer), s.contactExpected

	var shouldFlush bool
	if s.contactExpected > 0 && len(s.contactBuffer) >= s.contactExpected {
		shouldFlush = true
	} else if s.contactBufferTimer == nil {
		// 첫 데이터 수신 시 5초 타임아웃 타이머 시작 (이벤트 누락 대비)
		s.contactBufferTimer = time.AfterFunc(5*time.Second, func() {
			zap.S().Warn("[SyncService] ContactList 타임아웃(5초). 수신된 Contact까지만 업데이트합니다.")
			s.flushContactBuffer(ctx)
		})
	}
	s.contactMu.Unlock()

	zap.S().Debugf("[ContactList] 버퍼 저장: %s -> IP=%s, UA=%s (버퍼: %d, 예상: %d)", endpoint, ip, userAgent, bufLen, expected)

	if shouldFlush {
		s.flushContactBuffer(ctx)
	}
}

// handlerContactListComplete Contact 조회 완료 이벤트를 처리합니다.
func (s *SyncService) handlerContactListComplete(ctx context.Context, msg ami.Message) {
	listItemsStr := msg["ListItems"]
	zap.S().Debugf("[ContactListComplete] 완료 이벤트 수신 (ListItems: %q)", listItemsStr)
	listItems, err := strconv.Atoi(listItemsStr)
	if err != nil {
		zap.S().Warnf("[ContactListComplete] ListItems 파싱 실패: %v. 즉시 업데이트합니다.", err)
		s.flushContactBuffer(ctx)
		return
	}

	s.contactMu.Lock()
	s.contactExpected = listItems
	shouldFlush := len(s.contactBuffer) >= s.contactExpected
	currentCount := len(s.contactBuffer)
	s.contactMu.Unlock()

	if shouldFlush {
		s.flushContactBuffer(ctx)
	} else {
		zap.S().Debugf("[ContactListComplete] 추가 Contact 대기 중 (예상: %d, 현재: %d)", listItems, currentCount)
	}
}

// flushContactBuffer 수집된 Contact 목록을 Pipeline으로 Redis에 일괄 업데이트합니다.
func (s *SyncService) flushContactBuffer(ctx context.Context) {
	s.contactMu.Lock()

	buffer := s.contactBuffer
	s.contactBuffer = make(map[string]ContactItem)
	s.contactExpected = 0

	if s.contactBufferTimer != nil {
		s.contactBufferTimer.Stop()
		s.contactBufferTimer = nil
	}

	s.contactMu.Unlock()

	if len(buffer) == 0 {
		zap.S().Debug("[SyncService] Contact 버퍼가 비어 있어 업데이트를 건너뜁니다.")
		return
	}

	zap.S().Infof("[ContactList] Redis에 Contact %d개를 업데이트합니다. (Pipeline)", len(buffer))

	contacts := make([]ContactItem, 0, len(buffer))
	for _, item := range buffer {
		contacts = append(contacts, item)
	}

	// 파이프라인 내에서는 Script.Run의 NOSCRIPT 폴백이 동작하지 않으므로, 사전에 스크립트를 Redis에 로드합니다.
	if err := updateContactScript.Load(ctx, s.redis).Err(); err != nil {
		zap.S().Errorf("[Redis] Contact 업데이트 스크립트 로드 실패: %v", err)
		return
	}

	const batchSize = 500
	for i := 0; i < len(contacts); i += batchSize {
		end := i + batchSize
		if end > len(contacts) {
			end = len(contacts)
		}
		chunk := contacts[i:end]

		pipe := s.redis.Pipeline()
		for _, item := range chunk {
			reachKey := fmt.Sprintf("asterisk:exten:%s:reachability", item.Endpoint)
			updateContactScript.EvalSha(ctx, pipe, []string{reachKey}, item.UserAgent, item.IPAddress)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			zap.S().Errorf("[Redis] ContactList Pipeline 업데이트 실패: %v", err)
		}
	}

	zap.S().Infof("[ContactList] Contact %d개의 UserAgent를 업데이트했습니다.", len(contacts))
}

// handlerContactStateChange 장치 상태 변화(ContactStatus)의 이벤트를 처리합니다.
func (s *SyncService) handlerContactStateChange(ctx context.Context, msg ami.Message) {

	device := msg["EndpointName"]
	state := msg["ContactStatus"]
	uri := msg["URI"]

	if device == "" || state == "" {
		zap.S().Warnf("[ContactState] 이벤트에 사용 가능한 데이터가 없습니다. (EndpointName=%q, ContactStatus=%q, URI=%q)", device, state, uri)
		return
	}

	// URI에서 IP주소 조회
	ipv4 := extractSipIPv4(uri)

	// ContactStatus 문자열을 표준 ReachableState 변환
	reachableState := parseContactState(state)

	// ReachableState의 상태 값이 'ReachableStateUnavailable'이면 DeviceState을 'DeviceStateUnavailable'로 설정
	deviceState := DeviceStateIdle
	if reachableState == ReachableStateUnavailable {
		deviceState = DeviceStateUnavailable
	}

	zap.S().Debugf("[ContactState] 상태 매핑: %s ContactStatus=%s -> Reachable=%s, DeviceState=%s", device, state, reachableState, deviceState)

	// 기존 Redis에 저장된 user_agent 확인
	prevUA, _ := s.redis.HGet(ctx, fmt.Sprintf("asterisk:exten:%s:reachability", device), "user_agent").Result()

	zap.S().Infof("[ContactState] %s : %s(%s, UA: %s)", device, state, ipv4, prevUA)

	// Redis 상태 저장 및 이벤트 발행을 Pipeline으로 일괄(1회 RTT) 처리
	s.saveEndpointStateAndPublish(ctx, device, deviceState, reachableState, ipv4, prevUA)

	// 단말이 Reachable 상태인데 기존 Redis에 UserAgent가 없는 경우 (신규 등록 단말) 조회를 트리거
	if reachableState == ReachableStateAvailable && prevUA == "" {
		s.triggerDebouncedContactSync(ctx)
	}
}

// saveEndpointStateAndPublish 단말의 DeviceState와 ReachableState를 Pipeline으로 한 번에 저장 및 발행합니다.
func (s *SyncService) saveEndpointStateAndPublish(ctx context.Context, exten string,
	devState DeviceState, reachState ReachableState, ipaddr string, userAgent string) {

	nowKST := time.Now().Local().Format(time.RFC3339)
	callClass := "Normal"

	pipe := s.redis.Pipeline()

	// DeviceState HSet
	devKey := fmt.Sprintf("asterisk:exten:%s:device_state", exten)
	pipe.HSet(ctx, devKey,
		"state", string(devState),
		"call_class", callClass,
		"connected_line_num", "",
		"connected_line_name", "")

	// DeviceState Pub/Sub
	devEvent := DeviceStateChangeEvent{
		EventType:         "DeviceState",
		CallClass:         callClass,
		Exten:             exten,
		State:             string(devState),
		ConnectedLineNum:  "",
		ConnectedLineName: "",
		Timestamp:         nowKST,
	}
	if devPayload, err := json.Marshal(devEvent); err == nil {
		pipe.Publish(ctx, "asterisk:device_state", devPayload)
	}

	// Reachability HSet
	reachKey := fmt.Sprintf("asterisk:exten:%s:reachability", exten)
	pipe.HSet(ctx, reachKey,
		"state", string(reachState),
		"ip_address", ipaddr,
		"user_agent", userAgent)

	// Reachability Pub/Sub
	reachEvent := ReachableStateChangeEvent{
		EventType: "ReachableState",
		Exten:     exten,
		State:     string(reachState),
		IPAddress: ipaddr,
		UserAgent: userAgent,
		Timestamp: nowKST,
	}
	if reachPayload, err := json.Marshal(reachEvent); err == nil {
		pipe.Publish(ctx, "asterisk:reachable_state", reachPayload)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		zap.S().Errorf("[Redis] 단말 상태 Pipeline 저장 실패 (Exten: %s): %v", exten, err)
	}
}

// triggerDebouncedContactSync 신규 등록 단말 발생 시 1초 Debounce 후 Contact 조회를 실행합니다.
func (s *SyncService) triggerDebouncedContactSync(ctx context.Context) {
	s.bufferMu.Lock()
	defer s.bufferMu.Unlock()

	if s.contactSyncTimer != nil {
		s.contactSyncTimer.Stop()
	}

	s.contactSyncTimer = time.AfterFunc(1*time.Second, func() {
		zap.S().Info("[SyncService] UserAgent가 없는 신규 단말을 발견했습니다. Contact 목록(PJSIPShowContacts)을 요청합니다.")
		s.bufferMu.Lock()
		s.contactsRequested = false
		s.bufferMu.Unlock()
		s.requestContacts(ctx)
	})
}

// handlerNewStateChange 장치의 통화 상태 변경(Newstate) 이벤트를 처리합니다.
func (s *SyncService) handlerNewStateChange(ctx context.Context, msg ami.Message) {

	exten := extractExtenFromChannel(msg["Channel"])
	stateStr := msg["ChannelState"]
	connectedLineNum := msg["ConnectedLineNum"]
	connectedLineName := msg["ConnectedLineName"]

	if exten == "" || strings.Contains(exten, "unknown") {
		zap.S().Warnf("[NewState] 내선번호를 확인할 수 없어 무시: Channel=%q, ChannelState=%q", msg["Channel"], stateStr)
		return
	}
	if stateStr == "" {
		zap.S().Warnf("[NewState] ChannelState 값이 비어 있어 무시: Exten=%s, Channel=%q", exten, msg["Channel"])
		return
	}

	stateInt, err := strconv.Atoi(stateStr)
	if err != nil {
		zap.S().Warnf("[NewState] ChannelState 숫자 변환 실패: %v", err)
		return
	}

	// 이벤트 필터링 (Ring, Ringing, Up, Busy 만 처리)
	if !slices.Contains([]int{chanStateRing, chanStateRinging, chanStateUp, chanStateBusy}, stateInt) {
		zap.S().Debugf("[NewState] 처리 대상이 아닌 채널 상태로 무시: %s (ChannelState=%d, Channel=%s)", exten, stateInt, msg["Channel"])
		return
	}

	// 정수형 ChannelState를 표준 DeviceState로 변환
	deviceState := parseChannelState(stateInt)

	logger.Verbosef("[NewState] 상세: Channel=%s, ChannelState=%d, ConnectedLineName=%q", msg["Channel"], stateInt, connectedLineName)
	zap.S().Infof("[NewState] %s(%s) -> %s", exten, deviceState, connectedLineNum)
	s.updateDeviceState(ctx, exten, connectedLineNum, connectedLineName, deviceState)
}

// handlerHangup 통화 종료(Hangup) 이벤트를 처리합니다.
func (s *SyncService) handlerHangup(ctx context.Context, msg ami.Message) {

	exten := extractExtenFromChannel(msg["Channel"])
	deviceState := msg["ChannelStateDesc"]
	if exten == "" || strings.Contains(exten, "unknown") {
		zap.S().Warnf("[Hangup] 내선번호를 확인할 수 없어 무시: Channel=%s", msg["Channel"])
		return
	}

	logger.Verbosef("[Hangup] 상세: Channel=%s, Cause=%s(%s)", msg["Channel"], msg["Cause"], msg["Cause-txt"])
	zap.S().Infof("[Hangup] %s 통화 종료 (%s)", exten, deviceState)
	s.updateDeviceState(ctx, exten, "", "", DeviceStateIdle)
}

// updateDeviceState Redis에 장치 상태를 저장합니다.
func (s *SyncService) updateDeviceState(ctx context.Context, exten string,
	connectedLineNum string, connectedLineName string, state DeviceState) {

	// [통화 Class 설정]
	// 기본은 'Normal'로 설정하고, ConnectedLineName이 'Broadcasting'인 경우 'Broadcast'로 설정
	callClass := "Normal"
	if strings.Contains(connectedLineName, "Broadcasting") {
		callClass = "Broadcast"
	}

	// [Redis HASH 설정]
	key := fmt.Sprintf("asterisk:exten:%s:device_state", exten)
	err := s.redis.HSet(ctx, key,
		"state", string(state),
		"call_class", callClass,
		"connected_line_num", connectedLineNum,
		"connected_line_name", connectedLineName).Err()

	if err != nil {
		zap.S().Errorf("[Redis] DeviceState 저장 실패 (Exten: %s, State: %s): %v", exten, state, err)
		return
	}

	// [상태 변경 이벤트 발행 (Pub/Sub)]
	event := DeviceStateChangeEvent{
		EventType:         "DeviceState",
		CallClass:         callClass,
		Exten:             exten,
		State:             string(state),
		ConnectedLineNum:  connectedLineNum,
		ConnectedLineName: connectedLineName,
		Timestamp:         time.Now().Local().Format(time.RFC3339),
	}

	s.publishEvent(ctx, "asterisk:device_state", "DeviceState", event)
}

// updateReachableState Redis에 네트워크 상태를 저장합니다.
func (s *SyncService) updateReachableState(ctx context.Context,
	exten string, state ReachableState, ipaddr string, userAgent string) {

	// [Redis HASH 설정]
	key := fmt.Sprintf("asterisk:exten:%s:reachability", exten)
	err := s.redis.HSet(ctx, key,
		"state", string(state),
		"ip_address", ipaddr,
		"user_agent", userAgent).Err()

	if err != nil {
		zap.S().Errorf("[Redis] ReachableState 저장 실패 (Exten: %s, State: %s): %v", exten, state, err)
		return
	}

	// [상태 변경 이벤트 발행 (Pub/Sub)]
	event := ReachableStateChangeEvent{
		EventType: "ReachableState",
		Exten:     exten,
		State:     string(state),
		IPAddress: ipaddr,
		UserAgent: userAgent,
		Timestamp: time.Now().Local().Format(time.RFC3339),
	}

	s.publishEvent(ctx, "asterisk:reachable_state", "ReachableState", event)
}

// publishEvent 이벤트를 직렬화하여 지정된 채널에 발행합니다.
func (s *SyncService) publishEvent(ctx context.Context, channel string, eventName string, event any) {
	payload, err := json.Marshal(event)
	if err != nil {
		zap.S().Errorf("[Redis] %s 이벤트 직렬화 실패: %v", eventName, err)
		return
	}

	if err := s.redis.Publish(ctx, channel, payload).Err(); err != nil {
		zap.S().Errorf("[Redis] %s 이벤트 발행 실패 (Channel: %s): %v", eventName, channel, err)
	}
}

// parseDeviceState DeviceState 문자열을 표준 DeviceState 변환합니다.
func parseDeviceState(state string) DeviceState {
	switch state {
	case "Not in use":
		return DeviceStateIdle
	case "In use":
		return DeviceStateUse
	case "Busy":
		return DeviceStateBusy
	case "Ringing":
		return DeviceStateRinging
	case "Ring+Inuse":
		return DeviceStateRingUse
	case "On Hold":
		return DeviceStateHold
	case "Unavailable":
		return DeviceStateUnavailable
	case "Unknown", "Invalid":
		return DeviceStateUnknown
	default:
		return DeviceStateUnknown
	}
}

// parseContactState ContactStatus 문자열을 표준 ReachableState 변환합니다.
func parseContactState(state string) ReachableState {
	switch state {
	case "Reachable", "Updated":
		return ReachableStateAvailable
	case "Unreachable", "Removed":
		return ReachableStateUnavailable
	case "Unknown", "Unqualified":
		return ReachableStateUnknown
	default:
		return ReachableStateUnknown
	}
}

// parseChannelState 정수형 ChannelState를 표준 DeviceState로 변환합니다.
func parseChannelState(state int) DeviceState {
	switch state {
	case chanStateRing, chanStateRinging:
		return DeviceStateRinging
	case chanStateUp:
		return DeviceStateUse
	case chanStateBusy:
		return DeviceStateBusy
	default:
		return DeviceStateUnknown
	}
}

// extractSipIPv4는 `sip:` 뒤에 오는 IPv4 주소를 반환.
func extractSipIPv4(s string) string {
	// `sip:`뒤에 오는 IPv4 패턴 탐색
	ipPattern := `(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)`
	re := regexp.MustCompile(`sip:(?:[^@]+@)?(` + ipPattern + `)`)

	// ip 확인
	matches := re.FindStringSubmatch(s)

	if len(matches) <= 0 {
		return ""
	}

	return matches[1]
}

// extractExtenFromChannel 채널 정보(예: PJSIP/3000-000000e6)에서 내선번호를 추출합니다.
func extractExtenFromChannel(channel string) string {
	// `TECH/EXTEN-ID` 형식에서 내선번호(EXTEN) 추출
	re := regexp.MustCompile(`^[A-Za-z0-9_]+/([^-@]+)`)
	matches := re.FindStringSubmatch(channel)

	if len(matches) <= 1 {
		return ""
	}

	return matches[1]
}
