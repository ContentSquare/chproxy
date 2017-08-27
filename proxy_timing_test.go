package main

import (
	"math/rand"
	"net/url"
	"testing"

	"github.com/hagen1778/chproxy/config"
)

func BenchmarkReverseProxy_ServeHTTP(b *testing.B) {
	addr, err := url.Parse(fakeServer.URL)
	if err != nil {
		b.Fatalf("unexpected error: %s", err)
	}

	goodCfg.Clusters[0].Nodes = []string{addr.Host}
	proxy, err := NewReverseProxy(goodCfg)
	if err != nil {
		b.Fatalf("unexpected error: %s", err)
	}

	b.Run("parallel requests", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				makeRequest(proxy)
			}
		})
	})

	b.Run("parallel requests with config reloading", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				go proxy.ApplyConfig(newConfig())
				makeRequest(proxy)
			}
		})
	})
}

func newConfig() *config.Config {
	newCfg := *goodCfg
	newCfg.InitialUsers = []config.InitialUser{
		{
			Name:                 "default",
			MaxConcurrentQueries: rand.Uint32(),
		},
	}

	return &newCfg
}
