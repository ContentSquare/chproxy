package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/hagen1778/chproxy/config"
	"github.com/hagen1778/chproxy/log"
	"net"
	"strings"
)

func TestMain(m *testing.M) {
	log.SuppressOutput(true)
	retCode := m.Run()
	log.SuppressOutput(false)
	os.Exit(retCode)
}

var goodCfg = &config.Config{
	Clusters: []config.Cluster{
		{
			Name:   "cluster",
			Scheme: "http",
			Nodes:  []string{"localhost:8123"},
			ClusterUsers: []config.ClusterUser{
				{
					Name: "web",
				},
			},
		},
	},
	Users: []config.User{
		{
			Name:      "default",
			ToCluster: "cluster",
			ToUser:    "web",
		},
	},
}

func TestNewReverseProxy(t *testing.T) {
	proxy, err := NewReverseProxy(goodCfg)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if len(proxy.clusters) != 1 {
		t.Fatalf("got %d hosts; expected: %d", len(proxy.clusters), 1)
	}

	c := proxy.clusters["cluster"]
	if len(c.hosts) != 1 {
		t.Fatalf("got %d hosts; expected: %d", len(c.hosts), 1)
	}

	if c.hosts[0].addr.Host != "localhost:8123" {
		t.Fatalf("got %s host; expected: %s", c.hosts[0].addr.Host, "localhost:8123")
	}

	if len(proxy.users) != 1 {
		t.Fatalf("got %d users; expected: %d", len(proxy.users), 1)
	}

	if _, ok := proxy.users["default"]; !ok {
		t.Fatalf("expected user %q to be present in users", "default")
	}
}

var badCfg = &config.Config{
	Clusters: []config.Cluster{
		{
			Name:   "badCfg",
			Scheme: "http",
			Nodes:  []string{"localhost:8123"},
			ClusterUsers: []config.ClusterUser{
				{
					Name: "default",
				},
			},
		},
	},
	Users: []config.User{
		{
			Name:      "default",
			ToCluster: "cluster",
			ToUser:    "foo",
		},
	},
}

func TestApplyConfig(t *testing.T) {
	proxy, err := NewReverseProxy(goodCfg)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if err = proxy.ApplyConfig(badCfg); err == nil {
		t.Fatalf("error expected; got nil")
	}

	if _, ok := proxy.clusters["badCfg"]; ok {
		t.Fatalf("bad config applied; expected previous config")
	}
}

var authCfg = &config.Config{
	Clusters: []config.Cluster{
		{
			Name:   "cluster",
			Scheme: "http",
			Nodes:  []string{"localhost:8123"},
			ClusterUsers: []config.ClusterUser{
				{
					Name: "web",
				},
			},
		},
	},
	Users: []config.User{
		{
			Name:      "foo",
			Password:  "bar",
			ToCluster: "cluster",
			ToUser:    "web",
		},
	},
}

func TestReverseProxy_ServeHTTP(t *testing.T) {
	t.Run("Ok response", func(t *testing.T) {
		proxy := getProxy(t, goodCfg)

		expected := "Ok\n"
		resp := makeRequest(proxy)
		if resp != expected {
			t.Fatalf("expected response: %q; got: %q", expected, resp)
		}
	})

	t.Run("max concurrent queries for cluster user", func(t *testing.T) {
		proxy := getProxy(t, goodCfg)
		proxy.clusters["cluster"].users["web"].maxConcurrentQueries = 1
		go makeHeavyRequest(proxy, time.Millisecond*20)
		time.Sleep(time.Millisecond * 10)

		expected := "limits for cluster user \"web\" are exceeded: maxConcurrentQueries limit: 1"
		resp := makeRequest(proxy)
		if resp != expected {
			t.Fatalf("expected response: %q; got: %q", expected, resp)
		}
	})

	t.Run("max cluster time for cluster user", func(t *testing.T) {
		proxy := getProxy(t, goodCfg)
		proxy.clusters["cluster"].users["web"].maxExecutionTime = time.Millisecond * 10

		expected := "timeout for cluster user \"web\" exceeded: 10ms"
		resp := makeHeavyRequest(proxy, time.Millisecond*20)
		if resp != expected {
			t.Fatalf("expected response: %q; got: %q", expected, resp)
		}
	})

	t.Run("max concurrent queries for user", func(t *testing.T) {
		proxy := getProxy(t, goodCfg)
		proxy.users["default"].maxConcurrentQueries = 1
		go makeHeavyRequest(proxy, time.Millisecond*20)
		time.Sleep(time.Millisecond * 10)

		expected := "limits for user \"default\" are exceeded: maxConcurrentQueries limit: 1"
		resp := makeRequest(proxy)
		if resp != expected {
			t.Fatalf("expected response: %q; got: %q", expected, resp)
		}
	})

	t.Run("max cluster time for user", func(t *testing.T) {
		proxy := getProxy(t, goodCfg)
		proxy.users["default"].maxExecutionTime = time.Millisecond * 10

		expected := "timeout for user \"default\" exceeded: 10ms"
		resp := makeHeavyRequest(proxy, time.Millisecond*20)
		if resp != expected {
			t.Fatalf("expected response: %q; got: %q", expected, resp)
		}
	})

	t.Run("basicauth wrong name", func(t *testing.T) {
		proxy := getProxy(t, authCfg)

		req := httptest.NewRequest("POST", fakeServer.URL, nil)
		req.SetBasicAuth("fooo", "bar")
		resp := makeCustomRequest(proxy, req)

		expected := "invalid username or password for user \"fooo\""
		if resp != expected {
			t.Fatalf("expected response: %q; got: %q", expected, resp)
		}
	})

	t.Run("basicauth wrong pass", func(t *testing.T) {
		proxy := getProxy(t, authCfg)

		req := httptest.NewRequest("POST", fakeServer.URL, nil)
		req.SetBasicAuth("foo", "baar")
		resp := makeCustomRequest(proxy, req)

		expected := "invalid username or password for user \"foo\""
		if resp != expected {
			t.Fatalf("expected response: %q; got: %q", expected, resp)
		}
	})

	t.Run("basicauth success", func(t *testing.T) {
		proxy := getProxy(t, authCfg)

		req := httptest.NewRequest("POST", fakeServer.URL, nil)
		req.SetBasicAuth("foo", "bar")
		resp := makeCustomRequest(proxy, req)

		expected := "Ok\n"
		if resp != expected {
			t.Fatalf("expected response: %q; got: %q", expected, resp)
		}
	})

	t.Run("auth wrong name", func(t *testing.T) {
		proxy := getProxy(t, authCfg)

		uri := fmt.Sprintf("%s?user=fooo&password=bar", fakeServer.URL)
		req := httptest.NewRequest("POST", uri, nil)
		resp := makeCustomRequest(proxy, req)

		expected := "invalid username or password for user \"fooo\""
		if resp != expected {
			t.Fatalf("expected response: %q; got: %q", expected, resp)
		}
	})

	t.Run("auth wrong pass", func(t *testing.T) {
		proxy := getProxy(t, authCfg)

		uri := fmt.Sprintf("%s?user=foo&password=baar", fakeServer.URL)
		req := httptest.NewRequest("POST", uri, nil)
		resp := makeCustomRequest(proxy, req)

		expected := "invalid username or password for user \"foo\""
		if resp != expected {
			t.Fatalf("expected response: %q; got: %q", expected, resp)
		}
	})

	t.Run("auth success", func(t *testing.T) {
		proxy := getProxy(t, authCfg)

		uri := fmt.Sprintf("%s?user=foo&password=bar", fakeServer.URL)
		req := httptest.NewRequest("POST", uri, nil)
		resp := makeCustomRequest(proxy, req)

		expected := "Ok\n"
		if resp != expected {
			t.Fatalf("expected response: %q; got: %q", expected, resp)
		}
	})
}

func TestReverseProxy_ServeHTTP2(t *testing.T) {
	testCases := []struct {
		name            string
		allowedNetworks config.Networks
		expected        string
	}{
		{
			name:            "empty allowed networks",
			allowedNetworks: config.Networks{},
			expected:        "Ok\n",
		},
		{
			name:            "allow addr",
			allowedNetworks: config.Networks{getNetwork("192.0.2.1")},
			expected:        "Ok\n",
		},
		{
			name:            "allow addr by mask",
			allowedNetworks: config.Networks{getNetwork("192.0.2.1/32")},
			expected:        "Ok\n",
		},
		{
			name:            "disallow addr",
			allowedNetworks: config.Networks{getNetwork("192.0.2.2/32"), getNetwork("192.0.2.2")},
			expected:        "user \"default\" is not allowed to access from 192.0.2.1:1234",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			goodCfg.Users[0].Networks = tc.allowedNetworks
			proxy := getProxy(t, goodCfg)
			resp := makeRequest(proxy)

			if resp != tc.expected {
				t.Fatalf("expected response: %q; got: %q", tc.expected, resp)
			}
		})
	}
}

func getNetwork(s string) *net.IPNet {
	if !strings.Contains(s, `/`) {
		s += "/32"
	}

	_, ipnet, _ := net.ParseCIDR(s)

	return ipnet
}

var fakeServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}
	r.Body.Close()

	if len(body) > 0 {
		d, err := time.ParseDuration(string(body))
		if err != nil {
			fmt.Fprintln(w, "Err delay:", err)
			return
		}

		time.Sleep(d)
	}

	fmt.Fprintln(w, "Ok")
}))

func makeRequest(p *reverseProxy) string {
	return makeHeavyRequest(p, time.Duration(0))
}

func makeHeavyRequest(p *reverseProxy, duration time.Duration) string {
	body := bytes.NewBufferString(duration.String())
	req := httptest.NewRequest("POST", fakeServer.URL, body)
	rw := httptest.NewRecorder()
	p.ServeHTTP(rw, req)
	resp := rw.Result()
	response, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	return string(response)
}

func makeCustomRequest(p *reverseProxy, req *http.Request) string {
	rw := httptest.NewRecorder()
	p.ServeHTTP(rw, req)
	resp := rw.Result()
	response, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	return string(response)
}

func getProxy(t *testing.T, cfg *config.Config) *reverseProxy {
	addr, err := url.Parse(fakeServer.URL)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	cfg.Clusters[0].Nodes = []string{addr.Host}
	proxy, err := NewReverseProxy(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	return proxy
}
