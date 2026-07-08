package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestSetDefaultsIncludesAlpineUpstreams(t *testing.T) {
	v := viper.New()
	setDefaults(v)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		t.Fatalf("unmarshal defaults: %v", err)
	}

	if len(cfg.Alpine.Upstreams) != 2 {
		t.Fatalf("len(cfg.Alpine.Upstreams) = %d, want 2", len(cfg.Alpine.Upstreams))
	}

	first := cfg.Alpine.Upstreams[0]
	if first.Name != "tuna" {
		t.Errorf("first.Name = %q, want tuna", first.Name)
	}
	if first.URL != "https://mirrors.tuna.tsinghua.edu.cn/alpine" {
		t.Errorf("first.URL = %q, want TUNA Alpine mirror", first.URL)
	}
	if first.Priority != 1 {
		t.Errorf("first.Priority = %d, want 1", first.Priority)
	}

	second := cfg.Alpine.Upstreams[1]
	if second.Name != "official" {
		t.Errorf("second.Name = %q, want official", second.Name)
	}
	if second.URL != "https://dl-cdn.alpinelinux.org/alpine" {
		t.Errorf("second.URL = %q, want official Alpine mirror", second.URL)
	}
	if second.Priority != 2 {
		t.Errorf("second.Priority = %d, want 2", second.Priority)
	}
}
