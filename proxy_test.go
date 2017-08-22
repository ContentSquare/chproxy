package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/hagen1778/chproxy/config"
	"github.com/hagen1778/chproxy/log"
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
		Nodes: []string{"localhost:8123"},
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
		t.Fatalf("got %s host; expected: %d", proxy.targets[0].addr.Host, "localhost:8123")
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
			Nodes: []string{"127.0.0.1:8123", "127.0.0.2:8123"},
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

	goodCfg.Cluster.Nodes = []string{addr.Host}
	proxy, err := NewReverseProxy(goodCfg)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	t.Run("Ok response", func(t *testing.T) {
		expected := "Ok\n"
		resp := makeRequest(proxy, fakeServer.URL)
		if resp != expected {
			t.Fatalf("expected response: %q; got: %q", expected, resp)
		}
	})

	t.Run("max concurrent queries", func(t *testing.T) {
		expected := "limits for user \"default\" are exceeded: maxConcurrentQueries limit exceeded: 1"
		go makeRequest(proxy, fakeServer.URL)
		time.Sleep(time.Millisecond * 10)
		resp := makeRequest(proxy, fakeServer.URL)
		if resp != expected {
			t.Fatalf("expected response: %q; got: %q", expected, resp)
		}

		time.Sleep(time.Millisecond * 40)
	})

	t.Run("max execution time", func(t *testing.T) {
		expected := "timeout for user \"default\" exceeded: 10ms"
		goodCfg.Users = []config.User{
			{
				Name:             "default",
				MaxExecutionTime: time.Millisecond * 10,
			},
		}
		proxy.ApplyConfig(goodCfg)
		resp := makeRequest(proxy, fakeServer.URL)
		if resp != expected {
			t.Fatalf("expected response: %q; got: %q", expected, resp)
		}
	})
}

func makeRequest(p *reverseProxy, addr string) string {
	req := httptest.NewRequest("POST", addr, nil)
	rw := httptest.NewRecorder()
	p.ServeHTTP(rw, req)
	resp := rw.Result()
	body, _ := ioutil.ReadAll(resp.Body)

	return string(body)
}
