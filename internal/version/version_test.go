package version

import (
	"strings"
	"testing"
)

func TestVersionInfo(t *testing.T) {
	info := Info()
	if !strings.HasPrefix(info, "state-sync ") {
		t.Errorf("expected Info() to start with 'state-sync ', got: %s", info)
	}

	details := Get()
	if details.Version != Version {
		t.Errorf("expected details.Version to be %s, got: %s", Version, details.Version)
	}
	if details.GoVersion == "" {
		t.Errorf("expected details.GoVersion to not be empty")
	}
	if details.Platform == "" {
		t.Errorf("expected details.Platform to not be empty")
	}
}
