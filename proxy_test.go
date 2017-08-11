package main

import (
	"testing"
	"github.com/hagen1778/chproxy/config"
)

func TestNewReverseProxy(t *testing.T) {
	cfg := &config.Config{
		Cluster: config.Cluster{
			Shards: []string{"localhost:8123"},
		},
	}
	proxy, err := NewReverseProxy(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if len(proxy.targets) != 1 {
		t.Fatalf("got %d targets; expected: %d", len(proxy.targets), 1)
	}

	if proxy.targets[0].addr.Host != "localhost:8123" {
		t.Fatalf("got %d host; expected: %d", proxy.targets[0].addr.Host, "localhost:8123")
	}

	if len(proxy.users) != 1 {
		t.Fatalf("got %d users; expected: %d", len(proxy.users), 1)
	}

	if _, ok := proxy.users["default"]; !ok {
		t.Fatalf("expected user %q to be present in users", "default")
	}
}

func TestApplyConfig(t *testing.T) {
	cfg := &config.Config{
		Cluster: config.Cluster{
			Shards: []string{"address"},
		},
	}
	proxy, err := NewReverseProxy(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}