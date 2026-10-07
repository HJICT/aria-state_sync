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
	"common_lib/redis"

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

	contactSyncTimer *time.Timer
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
		zap.S().Info("[SyncService] AMI 연결 감지. 전체 상태 동기화를 시작합니다.")
		s.triggerFullSync(ctx)
	})

	// 주기적 전체 동기화 타이머 고루틴 시작
	go s.startPeriodicSync(ctx)
}

// startPeriodicSync 설정된 주기마다 전체 동기화 및 삭제 단말 정리를 실행합니다.
func (s *SyncService) startPeriodicSync(ctx context.Context) {
	ticker := time.NewTicker(s.syncInterval)
	defer ticker.Stop()

	zap.S().Infof("[SyncService] 주기적 동기화 타이머가 시작되었습니다 (간격: %v).", s.syncInterval)

	for {
		select {
		case <-ctx.Done():
			zap.S().Info("[SyncService] 주기적 동기화 고루틴을 종료합니다.")
			return
		case <-ticker.C:
			zap.S().Infof("[SyncService] 주기적 동기화 시간(%v) 도래. 전체 상태 동기화 및 삭제 단말 정리를 실행합니다.", s.syncInterval)
			s.triggerFullSync(ctx)
		}
	}
}

// triggerFullSync 전체 상태 동기화 요청(PJSIPShowEndpoints)을 전송합니다.
func (s *SyncService) triggerFullSync(ctx context.Context) {
	s.bufferMu.Lock()
	s.contactsRequested = false
	s.bufferMu.Unlock()

	zap.S().Info("[SyncService] 전체 상태 동기화 요청(PJSIPShowEndpoints)을 전송합니다.")
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
		zap.L().Warn("[EndPointList] 이벤트에서 조회된 내용이 없습니다.")
		return
	}

	// IP주소 조회
	ipaddr := extractSipIPv4(conacts)

	zap.S().Debugf("[EndPointList] %s : %s(%s)", device, state, ipaddr)

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

	var shouldFlush bool
	if s.expectedItems > 0 && len(s.endpointBuffer) >= s.expectedItems {
		shouldFlush = true
	} else if s.bufferTimer == nil {
		// 첫 데이터 수신 시 5초 타이머 시작 (이벤트 누락 대비)
		s.bufferTimer = time.AfterFunc(5*time.Second, func() {
			zap.S().Warn("[SyncService] EndpointList 수신 타임아웃(5초). 수신된 정보만 업데이트합니다.")
			s.flushEndpointBuffer(ctx)
		})
	}
	s.bufferMu.Unlock()

	if shouldFlush {
		zap.S().Infof("[EndPointList] 모든 장치 정보 수신 완료. Redis 업데이트를 수행합니다.")
		s.flushEndpointBuffer(ctx)
	}
}

// handlerEndpointListComplete 장치 초기 상태 조회 완료 이벤트를 처리합니다.
func (s *SyncService) handlerEndpointListComplete(ctx context.Context, msg ami.Message) {

	listItemsStr := msg["ListItems"]
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
		zap.S().Infof("[EndPointListComplete] 모든 장치(%d개) 수신 확인. Redis 업데이트를 시작합니다.", listItems)
		s.flushEndpointBuffer(ctx)
	} else {
		zap.S().Infof("[EndPointListComplete] 데이터 수신 대기 (기대: %d, 현재: %d). 완료 시까지 기다립니다.", listItems, currentCount)
	}
}

// flushEndpointBuffer EndpointList에서 조회된 단말 기준으로 Redis를 업데이트하고, 삭제된 단말을 제거한 뒤 ContactList 조회를 요청합니다.
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
		return
	}

	// 1. Asterisk에서 삭제된 단말 제거 (Redis 키 삭제)
	s.cleanupDeletedEndpoints(ctx, buffer)

	// 2. EndpointList에서 조회된 단말 기준으로 Redis 업데이트
	zap.S().Infof("[SyncService] %d개의 단말 정보를 Redis에 업데이트합니다.", len(buffer))

	for device, data := range buffer {
		s.updateDeviceState(ctx, device, "", "", data.DeviceState)

		// 기존 Redis에 저장되어 있는 UserAgent 유지 (ContactList 조회 전 정보 유실 방지)
		ua := data.UserAgent
		if ua == "" {
			if prevUA, err := s.redis.HGet(ctx, fmt.Sprintf("asterisk:exten:%s:reachability", device), "user_agent").Result(); err == nil {
				ua = prevUA
			}
		}
		s.updateReachableState(ctx, device, data.ReachableState, data.IPAddress, ua)
	}

	// 3. 이후 ContactList로 UserAgent 값을 업데이트
	s.requestContacts(ctx)
}

// cleanupDeletedEndpoints Asterisk에서 삭제된 단말을 감지하여 Redis에서 해당 키를 제거합니다.
func (s *SyncService) cleanupDeletedEndpoints(ctx context.Context, currentEndpoints map[string]EndpointData) {
	// Redis에 등록된 모든 asterisk:exten:* 키 조회
	keys, err := s.redis.Keys(ctx, "asterisk:exten:*").Result()
	if err != nil {
		zap.S().Warnf("[SyncService] 삭제 단말 정리 중 Redis 키 조회 실패: %v", err)
		return
	}

	deletedExtens := make(map[string]struct{})
	for _, key := range keys {
		// key 포맷: asterisk:exten:{exten}:device_state, asterisk:exten:{exten}:reachability
		parts := strings.Split(key, ":")
		if len(parts) >= 3 && parts[0] == "asterisk" && parts[1] == "exten" {
			exten := parts[2]
			if _, exists := currentEndpoints[exten]; !exists {
				deletedExtens[exten] = struct{}{}
			}
		}
	}

	if len(deletedExtens) == 0 {
		return
	}

	for exten := range deletedExtens {
		deviceKey := fmt.Sprintf("asterisk:exten:%s:device_state", exten)
		reachKey := fmt.Sprintf("asterisk:exten:%s:reachability", exten)

		s.redis.Del(ctx, deviceKey, reachKey)
		zap.S().Infof("[SyncService] 삭제된 단말 정리 완료: exten=%s (Redis 키 제거)", exten)
	}

	zap.S().Infof("[SyncService] 총 %d개의 삭제된 단말 키를 Redis에서 제거했습니다.", len(deletedExtens))
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

	zap.S().Info("[SyncService] Contact 목록(PJSIPShowContacts) 조회를 요청하여 UserAgent를 업데이트합니다.")

	_, err := s.ami.Action(ami.Message{
		"Action": "PJSIPShowContacts",
	})
	if err != nil {
		zap.S().Errorf("[SyncService] PJSIPShowContacts 요청 실패: %v", err)
	}
}

// handlerContactList Contact 정보(UserAgent, IP 등) 이벤트를 수신하여 Redis의 UserAgent 값을 업데이트합니다.
func (s *SyncService) handlerContactList(ctx context.Context, msg ami.Message) {
	endpoint := msg["Endpoint"]
	userAgent := msg["UserAgent"]
	viaAddr := msg["ViaAddr"]
	uri := msg["Uri"]

	if endpoint == "" {
		return
	}

	zap.S().Debugf("[ContactList] %s : UA(%s), Via(%s)", endpoint, userAgent, viaAddr)

	if userAgent != "" {
		reachKey := fmt.Sprintf("asterisk:exten:%s:reachability", endpoint)
		// Redis에 해당 단말 키가 존재하는 경우 UserAgent 업데이트
		if s.redis.Exists(ctx, reachKey).Val() > 0 {
			fields := []any{"user_agent", userAgent}
			if viaAddr != "" {
				fields = append(fields, "ip_address", viaAddr)
			} else if uri != "" {
				if ip := extractSipIPv4(uri); ip != "" {
					fields = append(fields, "ip_address", ip)
				}
			}
			if err := s.redis.HSet(ctx, reachKey, fields...).Err(); err != nil {
				zap.S().Errorf("[Redis] ContactList UserAgent 갱신 실패 (exten: %s): %v", endpoint, err)
			}
		}
	}
}

// handlerContactListComplete Contact 조회 완료 이벤트를 처리합니다.
func (s *SyncService) handlerContactListComplete(ctx context.Context, msg ami.Message) {
	listItems := msg["ListItems"]
	zap.S().Infof("[ContactListComplete] Contact 목록 수신 완료 (등록된 Contact: %s개). UserAgent 업데이트 완료.", listItems)
}

// handlerContactStateChange 장치 상태 변화(ContactStatus)의 이벤트를 처리합니다.
func (s *SyncService) handlerContactStateChange(ctx context.Context, msg ami.Message) {

	device := msg["EndpointName"]
	state := msg["ContactStatus"]
	uri := msg["URI"]

	if device == "" || state == "" {
		zap.S().Warn("[ContactState] 이벤트에서 조회된 내용이 없습니다.")
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

	// 기존 Redis에 저장된 user_agent 확인
	prevUA, _ := s.redis.HGet(ctx, fmt.Sprintf("asterisk:exten:%s:reachability", device), "user_agent").Result()

	zap.S().Infof("[ContactState] %s : %s(%s, UA: %s)", device, state, ipv4, prevUA)

	// Redis에 상태 저장
	s.updateDeviceState(ctx, device, "", "", deviceState)
	s.updateReachableState(ctx, device, reachableState, ipv4, prevUA)

	// 단말이 Reachable 상태인데 기존 Redis에 UserAgent가 없는 경우 (신규 등록 단말) 조회를 트리거
	if reachableState == ReachableStateAvailable && prevUA == "" {
		s.triggerDebouncedContactSync(ctx)
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
		zap.S().Info("[SyncService] UserAgent가 없는 신규 단말 감지. Contact 목록(PJSIPShowContacts)을 조회합니다.")
		s.bufferMu.Lock()
		s.contactsRequested = false
		s.bufferMu.Unlock()
		s.requestContacts(ctx)
	})
}

// handlerNewStateChange 장치의 통화 상태 변경(Newstate) 이벤트를 처리합니다.
func (s *SyncService) handlerNewStateChange(ctx context.Context, msg ami.Message) {

	exten := msg["CallerIDNum"]
	stateStr := msg["ChannelState"]
	connectedLineNum := msg["ConnectedLineNum"]
	connectedLineName := msg["ConnectedLineName"]

	if (exten == "" || strings.Contains(exten, "unknown")) || stateStr == "" {
		zap.S().Warn("[NewState] 이벤트에서 조회된 내용이 없습니다.")
		return
	}

	stateInt, err := strconv.Atoi(stateStr)
	if err != nil {
		zap.S().Warnf("[NewState] 채널 상태값을 숫자변 변경 중 오류 발생. %v", err)
		return
	}

	// 이벤트 필터링 (Ring, Ringing, Up, Busy 만 처리)
	if !slices.Contains([]int{chanStateRing, chanStateRinging, chanStateUp, chanStateBusy}, stateInt) {
		return
	}

	// 정수형 ChannelState를 표준 DeviceState로 변환
	deviceState := parseChannelState(stateInt)

	zap.S().Infof("[NewState] %s(%s) -> %s", exten, stateStr, deviceState)
	s.updateDeviceState(ctx, exten, connectedLineNum, connectedLineName, deviceState)
}

// handlerHangup 통화 종료(Hangup) 이벤트를 처리합니다.
func (s *SyncService) handlerHangup(ctx context.Context, msg ami.Message) {

	exten := msg["CallerIDNum"]
	deviceState := msg["ChannelStateDesc"]
	if exten == "" || strings.Contains(exten, "unknown") {
		zap.S().Warnf("[Hangup] 내선번호를 추출할 수 없습니다. (Channel: %s)", msg["Channel"])
		return
	}

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
