package outputs

import "testing"

func TestNormalizeMessageName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "GlobalPositionInt", want: "GlobalPositionInt"},
		{name: "*common.MessageGlobalPositionInt", want: "GlobalPositionInt"},
		{name: "github.com/bluenviron/gomavlib/v2/pkg/dialects/common.MessageSysStatus", want: "SysStatus"},
		{name: "VFR_HUD", want: "VFR_HUD"},
		{name: "*", want: "*"},
	}

	for _, test := range tests {
		if got := NormalizeMessageName(test.name); got != test.want {
			t.Errorf("NormalizeMessageName(%q) = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestMessageFilterAllows(t *testing.T) {
	filter := MessageFilter{
		Include: []string{"GlobalPositionInt", "VFR_HUD"},
		Exclude: []string{"VFR_HUD"},
	}

	if !filter.Allows("*common.MessageGlobalPositionInt") {
		t.Fatal("expected GlobalPositionInt to be allowed")
	}
	if filter.Allows("VFR_HUD") {
		t.Fatal("expected excluded VFR_HUD to be blocked")
	}
	if filter.Allows("Heartbeat") {
		t.Fatal("expected non-included Heartbeat to be blocked")
	}
}

func TestMessageFilterWildcard(t *testing.T) {
	filter := MessageFilter{Include: []string{"*"}}

	if !filter.Allows("Heartbeat") {
		t.Fatal("expected wildcard to allow Heartbeat")
	}
	if !filter.Allows("*common.MessageGlobalPositionInt") {
		t.Fatal("expected wildcard to allow normalized Go type names")
	}
	if !filter.Allows("") {
		t.Fatal("expected wildcard to allow any message name")
	}
}

func TestMessageFilterEmptyIncludeMatchesNothing(t *testing.T) {
	filter := MessageFilter{}

	if filter.Allows("Heartbeat") {
		t.Fatal("expected an empty include list to reject named messages")
	}
	if filter.Allows("") {
		t.Fatal("expected an empty include list to reject unnamed messages")
	}
}

func TestMessageFilterBlankIncludesMatchNothing(t *testing.T) {
	filter := MessageFilter{Include: []string{"", "  "}}

	if filter.Allows("Heartbeat") {
		t.Fatal("expected blank include entries to reject messages")
	}
}
