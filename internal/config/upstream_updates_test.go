package config

import (
	"testing"
	"time"
)

func TestParseUpdateCheckInterval(t *testing.T) {
	tests := []struct {
		raw     string
		want    time.Duration
		enabled bool
		wantErr bool
	}{
		{"", time.Hour, true, false},
		{"1h", time.Hour, true, false},
		{"30m", 30 * time.Minute, true, false},
		{"off", 0, false, false},
		{"0", 0, false, false},
		{"-1h", 0, false, true},
		{"tomorrow", 0, false, true},
	}
	for _, tt := range tests {
		got, enabled, err := ParseUpdateCheckInterval(tt.raw)
		if (err != nil) != tt.wantErr || got != tt.want || enabled != tt.enabled {
			t.Errorf("ParseUpdateCheckInterval(%q) = (%v, %v, %v), want (%v, %v, err=%v)", tt.raw, got, enabled, err, tt.want, tt.enabled, tt.wantErr)
		}
	}
}
