package service

import (
	"testing"

	ami "common_lib/asterisk"
)

func TestExtractExtenFromChannel(t *testing.T) {
	tests := []struct {
		name     string
		channel  string
		expected string
	}{
		{
			name:     "PJSIP 채널 기본 형식",
			channel:  "PJSIP/3000-000000e6",
			expected: "3000",
		},
		{
			name:     "SIP 채널 형식",
			channel:  "SIP/1001-00000001",
			expected: "1001",
		},
		{
			name:     "도메인이 포함된 PJSIP 채널",
			channel:  "PJSIP/2000@default-000000a1",
			expected: "2000",
		},
		{
			name:     "ID 접미사가 없는 채널",
			channel:  "PJSIP/4000",
			expected: "4000",
		},
		{
			name:     "빈 문자열",
			channel:  "",
			expected: "",
		},
		{
			name:     "잘못된 형식",
			channel:  "unknown",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractExtenFromChannel(tt.channel)
			if got != tt.expected {
				t.Errorf("extractExtenFromChannel(%q) = %q, want %q", tt.channel, got, tt.expected)
			}
		})
	}
}

func TestCheckActionResponse(t *testing.T) {
	if err := checkActionResponse(ami.Message{"Response": "Success"}); err != nil {
		t.Errorf("Success 응답은 에러가 아니어야 합니다: %v", err)
	}
	if err := checkActionResponse(ami.Message{"Response": "Error", "Message": "Permission denied"}); err == nil {
		t.Error("Error 응답은 에러여야 합니다")
	}
	if err := checkActionResponse(ami.Message{}); err == nil {
		t.Error("Response 헤더가 없으면 에러여야 합니다")
	}
}
