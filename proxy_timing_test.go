package main

import (
	"testing"
	"net/http/httptest"
	"net/http"
	"fmt"
	"net/url"
	"github.com/hagen1778/chproxy/config"
	"math/rand"
)

func BenchmarkReverseProxy_ServeHTTP(b *testing.B) {
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Ok")
	}))
	defer fakeServer.Close()

	addr, err := url.Parse(fakeServer.URL)
	if err != nil {
		b.Fatalf("unexpected error: %s", err)
	}

	goodCfg.Cluster.Shards = []string{addr.Host}
	proxy, err := NewReverseProxy(goodCfg)
	if err != nil {
		b.Fatalf("unexpected error: %s", err)
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			makeRequest(proxy, fakeServer.URL)
		}
	})
}

func BenchmarkReverseProxy_ApplyConfig(b *testing.B) {
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Ok")
	}))
	defer fakeServer.Close()

	addr, err := url.Parse(fakeServer.URL)
	if err != nil {
		b.Fatalf("unexpected error: %s", err)
	}

	goodCfg.Cluster.Shards = []string{addr.Host}
	proxy, err := NewReverseProxy(goodCfg)
	if err != nil {
		b.Fatalf("unexpected error: %s", err)
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			proxy.ApplyConfig(newConfig())
			makeRequest(proxy, fakeServer.URL)
		}
	})
}

func newConfig() *config.Config {
	newCfg := *goodCfg
	newCfg.Users = []config.User{
		{
			Name: "default",
			MaxConcurrentQueries: rand.Uint32(),
		},
	}

	return &newCfg
}