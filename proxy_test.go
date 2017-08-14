package main

import (
	"fmt"
	"github.com/hagen1778/chproxy/config"
	"github.com/hagen1778/chproxy/log"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	log.SuppressOutput(true)
	retCode := m.Run()
	log.SuppressOutput(false)
	os.Exit(retCode)
}

var goodCfg = &config.Config{
	Cluster: config.Cluster{
		Scheme: "http",
		Shards: []string{"localhost:8123"},
	},
	Users: []config.User{
		{
			Name:                 "default",
			MaxConcurrentQueries: 1,
		},
	},
}

func TestNewReverseProxy(t *testing.T) {
	proxy, err := NewReverseProxy(goodCfg)
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
	proxy, err := NewReverseProxy(goodCfg)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	badCfg := &config.Config{
		Cluster: config.Cluster{
			Scheme: "udp",
			Shards: []string{"127.0.0.1:8123", "127.0.0.2:8123"},
		},
	}

	if err = proxy.ApplyConfig(badCfg); err == nil {
		t.Fatalf("error expected; got nil")
	}

	if len(proxy.targets) != 1 {
		t.Fatalf("bad config applied; expected previous config")
	}
}

func TestReverseProxy_ServeHTTP(t *testing.T) {
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Millisecond * 50)
		fmt.Fprintln(w, "Ok")
	}))
	defer fakeServer.Close()

	addr, err := url.Parse(fakeServer.URL)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	goodCfg.Cluster.Shards = []string{addr.Host}
	proxy, err := NewReverseProxy(goodCfg)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	var testCases = []struct {
		name    string
		message string
	}{
		{
			"Ok response",
			"Ok\n",
		},
		{
			"max concurrent queries",
			"limits for user \"default\" are exceeded: maxConcurrentQueries limit exceeded: 1",
		},
		{
			"max execution time",
			"context deadline exceeded",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			switch tc.name {
			case "Ok response":
				resp := makeRequest(proxy, fakeServer.URL)
				if resp != tc.message {
					t.Fatalf("expected response: %q; got: %q", tc.message, resp)
				}
			case "max concurrent queries":
				go makeRequest(proxy, fakeServer.URL)
				time.Sleep(time.Millisecond * 10)
				resp := makeRequest(proxy, fakeServer.URL)
				if resp != tc.message {
					t.Fatalf("expected response: %q; got: %q", tc.message, resp)
				}
				time.Sleep(time.Millisecond * 50)
			case "max execution time":
				goodCfg.Users = []config.User{
					{
						Name:             "default",
						MaxExecutionTime: time.Millisecond * 10,
					},
				}
				proxy.ApplyConfig(goodCfg)
				resp := makeRequest(proxy, fakeServer.URL)
				if resp != tc.message {
					t.Fatalf("expected response: %q; got: %q", tc.message, resp)
				}
			}
		})
	}
}

func makeRequest(p *reverseProxy, addr string) string {
	req := httptest.NewRequest("POST", addr, nil)
	rw := httptest.NewRecorder()
	p.ServeHTTP(rw, req)
	resp := rw.Result()
	body, _ := ioutil.ReadAll(resp.Body)

	return string(body)
}
