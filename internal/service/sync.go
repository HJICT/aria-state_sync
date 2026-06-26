package service

import (
	"context"
	"encoding/json"
	"fmt"
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
	Timestamp string `json:"timestamp"`
}

// EndpointData 는 Redis 업데이트 전 버퍼링을 위한 데이터 구조입니다.
type EndpointData struct {
	DeviceState    DeviceState
	ReachableState ReachableState
}

// SyncService Asterisk의 상태 정보를 Redis로 동기화하는 비즈니스 로직을 담당합니다.
type SyncService struct {
	ami   *ami.AmiClient
	redis *redis.RedisClient

	bufferMu       sync.Mutex
	endpointBuffer map[string]EndpointData
	bufferTimer    *time.Timer
	expectedItems  int
}

// NewSyncService 새로운 SyncService 인스턴스를 생성합니다.
func NewSyncService(ami *ami.AmiClient, redis *redis.RedisClient) *SyncService {
	return &SyncService{
		ami:            ami,
		redis:          redis,
		endpointBuffer: make(map[string]EndpointData),
	}
}

// Start AMI 이벤트를 구독하고 Redis 동기화 로직을 실행합니다.
func (s *SyncService) Start(ctx context.Context) {
	zap.S().Info("[SyncService] Asterisk-Redis 상태 동기화 서비스를 시작합니다.")

	// EndpointList 이벤트 처리 (초기 상태 정보 조회)
	s.ami.SubscribeEvent("EndpointList", func(msg ami.Message) {
		s.handlerEndpointList(ctx, msg)
	})

	// EndpointListComplete 이벤트 처리 (초기 상태 정보 조회 완료)
	s.ami.SubscribeEvent("EndpointListComplete", func(msg ami.Message) {
		s.handlerEndpointListComplete(ctx, msg)
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
		zap.S().Info("[SyncService] AMI 재연결 감지. 전체 상태 동기화 요청(PJSIPShowEndpoints)을 전송합니다.")

		_, err := s.ami.Action(ami.Message{
			"Action": "PJSIPShowEndpoints",
		})

		if err != nil {
			zap.S().Errorf("[SyncService] PJSIPShowEndpoints 요청 실패: %v", err)
		}
	})
}

// handlerEndpointList 장치의 초기 상태 정보 이벤트를 처리 합니다.
func (s *SyncService) handlerEndpointList(ctx context.Context, msg ami.Message) {
	device := msg["ObjectName"]
	state := msg["DeviceState"]

	if device == "" || state == "" {
		zap.L().Warn("[EndPointList] 이벤트에서 조회된 내용이 없습니다.")
		return
	}

	zap.S().Debugf("[EndPointList] %s : %s", device, state)

	// DeviceState 문자열을 표준 DeviceState 변환
	deviceState := parseDeviceState(state)

	// DeviceState의 상태값이 'DeviceStateUnavailable'이면 reachableState를 'ReachableStateUnavailable' 으로 설정
	reachableState := ReachableStateAvailable
	if deviceState == DeviceStateUnavailable {
		reachableState = ReachableStateUnavailable
	}

	s.bufferMu.Lock()
	// 버퍼에 데이터 저장
	s.endpointBuffer[device] = EndpointData{
		DeviceState:    deviceState,
		ReachableState: reachableState,
	}

	var shouldFlush bool
	if s.expectedItems > 0 && len(s.endpointBuffer) >= s.expectedItems {
		shouldFlush = true
	} else if s.bufferTimer == nil {
		// 첫 데이터 수신 시 3초 타이머 시작 (이벤트 누락 대비)
		s.bufferTimer = time.AfterFunc(3*time.Second, func() {
			zap.S().Warn("[SyncService] EndpointList 수신 타임아웃(3초). 수신된 정보만 업데이트합니다.")
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
	// 현재 버퍼 개수와 기대 개수 비교
	shouldFlush := len(s.endpointBuffer) >= s.expectedItems
	currentCount := len(s.endpointBuffer)
	s.bufferMu.Unlock()

	if shouldFlush {
		zap.S().Infof("[EndPointListComplete] 모든 장치(%d개) 수신 확인. 업데이트를 시작합니다.", listItems)
		s.flushEndpointBuffer(ctx)
	} else {
		zap.S().Infof("[EndPointListComplete] 데이터 수신 대기 (기대: %d, 현재: %d). 완료 시까지 기다립니다.", listItems, currentCount)
	}
}

// flushEndpointBuffer 버퍼링된 장치 정보를 한 번에 Redis에 업데이트합니다.
func (s *SyncService) flushEndpointBuffer(ctx context.Context) {

	s.bufferMu.Lock()

	// 현재 버퍼 복사 및 초기화
	buffer := s.endpointBuffer
	s.endpointBuffer = make(map[string]EndpointData)
	s.expectedItems = 0

	// 타이머가 실행 중인 경우(타임아웃으로 호출된 경우 등) 정리
	if s.bufferTimer != nil {
		s.bufferTimer.Stop()
		s.bufferTimer = nil
	}

	s.bufferMu.Unlock()

	if len(buffer) == 0 {
		return
	}

	zap.S().Infof("[SyncService] %d개의 장치 정보를 Redis에 일괄 업데이트합니다.", len(buffer))

	for device, data := range buffer {
		s.updateDeviceState(ctx, device, "", "", data.DeviceState)
		s.updateReachableState(ctx, device, data.ReachableState)
	}
}

// handlerContactStateChange 장치 상태 변화(ContactStatus)의 이벤트를 처리합니다.
func (s *SyncService) handlerContactStateChange(ctx context.Context, msg ami.Message) {

	device := msg["EndpointName"]
	state := msg["ContactStatus"]

	if device == "" || state == "" {
		zap.S().Warn("[ContactState] 이벤트에서 조회된 내용이 없습니다.")
		return
	}

	zap.S().Infof("[ContactState] %s : %s", device, state)

	// ContactStatus 문자열을 표준 ReachableState 변환
	reachableState := parseContactState(state)

	// ReachableState의 상태 값이 'ReachableStateUnavailable'이면 DeviceState을 'DeviceStateUnavailable'로 설정
	deviceState := DeviceStateIdle
	if reachableState == ReachableStateUnavailable {
		deviceState = DeviceStateUnavailable
	}

	// Redis에 상태 저장
	s.updateDeviceState(ctx, device, "", "", deviceState)
	s.updateReachableState(ctx, device, reachableState)
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
	if !slices.Contains([]int{4, 5, 6, 7}, stateInt) {
		return
	}

	// 정수형 ChannelState를 표준 DeviceState로 변환
	deviceState := parseChannelState(stateInt)

	zap.S().Infof("[NewState] %s(%s) -> %s", exten, stateInt, stateStr, deviceState)
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

	key := fmt.Sprintf("asterisk:exten:%s:device_state", exten)
	if err := s.redis.HSet(ctx, key, "state", string(state), "connected_line_num", connectedLineNum,
		"connected_line_name", connectedLineName).Err(); err != nil {

		zap.S().Errorf("[Redis] DeviceState 저장 실패 (Exten: %s, State: %s): %v", exten, state, err)
		return
	}

	// 통화 종류 (Call Class) 설정
	// 기본은 'Normal'로 설정하고, ConnectedLineName이 'Broadcasting'인 경우 'Broadcast'로 설정
	callClass := "Normal"
	if strings.Contains(connectedLineName, "Broadcasting") {
		callClass = "Broadcast"
	}

	// 상태 변경 이벤트 발행 (Pub/Sub)
	event := DeviceStateChangeEvent{
		EventType:         "DeviceState",
		CallClass:         callClass,
		Exten:             exten,
		State:             string(state),
		ConnectedLineNum:  connectedLineNum,
		ConnectedLineName: connectedLineName,
		Timestamp:         time.Now().Local().Format(time.RFC3339),
	}

	payload, err := json.Marshal(event)
	if err != nil {
		zap.S().Errorf("[Redis] DeviceState 이벤트 직렬화 실패: %v", err)
		return
	}

	if err := s.redis.Publish(ctx, "asterisk:device_state", payload).Err(); err != nil {
		zap.S().Errorf("[Redis] DeviceState 이벤트 발행 실패 (Channel: asterisk:device_state): %v", err)
	}
}

// updateReachableState Redis에 네트워크 상태를 저장합니다.
func (s *SyncService) updateReachableState(ctx context.Context, exten string, state ReachableState) {
	key := fmt.Sprintf("asterisk:exten:%s:reachability", exten)
	if err := s.redis.HSet(ctx, key, "state", string(state)).Err(); err != nil {
		zap.S().Errorf("[Redis] ReachableState 저장 실패 (Exten: %s, State: %s): %v", exten, state, err)
		return
	}

	// 상태 변경 이벤트 발행 (Pub/Sub)
	event := ReachableStateChangeEvent{
		EventType: "ReachableState",
		Exten:     exten,
		State:     string(state),
		Timestamp: time.Now().Local().Format(time.RFC3339),
	}

	payload, err := json.Marshal(event)
	if err != nil {
		zap.S().Errorf("[Redis] ReachableState 이벤트 직렬화 실패: %v", err)
		return
	}

	if err := s.redis.Publish(ctx, "asterisk:reachable_state", payload).Err(); err != nil {
		zap.S().Errorf("[Redis] ReachableState 이벤트 발행 실패 (Channel: asterisk:reachable_state): %v", err)
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
	case 4, 5: // Ring, Ringing
		return DeviceStateRinging
	case 6: // Up
		return DeviceStateUse
	case 7: // Busy
		return DeviceStateBusy
	default:
		return DeviceStateUnknown
	}
}
