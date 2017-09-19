package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/Vertamedia/chproxy/config"
	"github.com/Vertamedia/chproxy/log"
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

func newConfiguredProxy(cfg *config.Config) (*reverseProxy, error) {
	p := newReverseProxy()
	if err := p.ApplyConfig(cfg); err != nil {
		return p, fmt.Errorf("error while loading config: %s", err)
	}

	return p, nil
}

func TestNewReverseProxy(t *testing.T) {
	proxy := newReverseProxy()
	if err := proxy.ApplyConfig(goodCfg); err != nil {
		t.Fatalf("error while loading config: %s", err)
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
	proxy, err := newConfiguredProxy(goodCfg)
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
					Name:     "web",
					Password: "webpass",
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
	testCases := []struct {
		name     string
		expected string
		cfg      *config.Config
		f        func(p *reverseProxy) string
	}{
		{
			name:     "Ok response",
			expected: "Ok\n",
			cfg:      goodCfg,
			f:        func(p *reverseProxy) string { return makeRequest(p) },
		},
		{
			name:     "max concurrent queries for cluster user",
			expected: "limits for cluster user \"web\" are exceeded: maxConcurrentQueries limit: 1",
			cfg:      goodCfg,
			f: func(p *reverseProxy) string {
				p.clusters["cluster"].users["web"].maxConcurrentQueries = 1
				go makeHeavyRequest(p, time.Millisecond*20)
				time.Sleep(time.Millisecond * 10)
				return makeRequest(p)
			},
		},
		{
			name:     "max time for cluster user",
			expected: "timeout for cluster user \"web\" exceeded: 10ms",
			cfg:      goodCfg,
			f: func(p *reverseProxy) string {
				p.clusters["cluster"].users["web"].maxExecutionTime = time.Millisecond * 10
				return makeHeavyRequest(p, time.Millisecond*20)
			},
		},
		{
			name:     "choose max time between users",
			expected: "timeout for user \"default\" exceeded: 10ms",
			cfg:      goodCfg,
			f: func(p *reverseProxy) string {
				p.users["default"].maxExecutionTime = time.Millisecond * 10
				p.clusters["cluster"].users["web"].maxExecutionTime = time.Millisecond * 15
				return makeHeavyRequest(p, time.Millisecond*20)
			},
		},
		{
			name:     "choose max time between users2",
			expected: "timeout for cluster user \"web\" exceeded: 10ms",
			cfg:      goodCfg,
			f: func(p *reverseProxy) string {
				p.users["default"].maxExecutionTime = time.Millisecond * 15
				p.clusters["cluster"].users["web"].maxExecutionTime = time.Millisecond * 10
				return makeHeavyRequest(p, time.Millisecond*20)
			},
		},
		{
			name:     "max concurrent queries for user",
			expected: "limits for user \"default\" are exceeded: maxConcurrentQueries limit: 1",
			cfg:      goodCfg,
			f: func(p *reverseProxy) string {
				p.users["default"].maxConcurrentQueries = 1
				go makeHeavyRequest(p, time.Millisecond*20)
				time.Sleep(time.Millisecond * 10)
				return makeRequest(p)
			},
		},
		{
			name:     "basicauth wrong name",
			expected: "invalid username or password for user \"fooo\"",
			cfg:      authCfg,
			f: func(p *reverseProxy) string {
				req := httptest.NewRequest("POST", fakeServer.URL, nil)
				req.SetBasicAuth("fooo", "bar")
				return makeCustomRequest(p, req)
			},
		},
		{
			name:     "basicauth wrong pass",
			expected: "invalid username or password for user \"foo\"",
			cfg:      authCfg,
			f: func(p *reverseProxy) string {
				req := httptest.NewRequest("POST", fakeServer.URL, nil)
				req.SetBasicAuth("foo", "baar")
				return makeCustomRequest(p, req)
			},
		},
		{
			name:     "auth wrong name",
			expected: "invalid username or password for user \"fooo\"",
			cfg:      authCfg,
			f: func(p *reverseProxy) string {
				uri := fmt.Sprintf("%s?user=fooo&password=bar", fakeServer.URL)
				req := httptest.NewRequest("POST", uri, nil)
				return makeCustomRequest(p, req)
			},
		},
		{
			name:     "auth wrong name",
			expected: "invalid username or password for user \"foo\"",
			cfg:      authCfg,
			f: func(p *reverseProxy) string {
				uri := fmt.Sprintf("%s?user=foo&password=baar", fakeServer.URL)
				req := httptest.NewRequest("POST", uri, nil)
				return makeCustomRequest(p, req)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			proxy, err := getProxy(goodCfg)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			res := tc.f(proxy)
			if res != tc.expected {
				t.Fatalf("expected response: %q; got: %q", tc.expected, res)
			}
		})
	}

	t.Run("basicauth success", func(t *testing.T) {
		proxy, err := getProxy(authCfg)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		req := httptest.NewRequest("POST", fakeServer.URL, nil)
		req.SetBasicAuth("foo", "bar")
		resp := makeCustomRequest(proxy, req)

		expected := "Ok\n"
		if resp != expected {
			t.Fatalf("expected response: %q; got: %q", expected, resp)
		}

		user, pass := getAuth(req)
		if user != authCfg.Clusters[0].ClusterUsers[0].Name {
			t.Fatalf("user name expected to be %q; got %q", authCfg.Clusters[0].ClusterUsers[0].Name, user)
		}

		if pass != authCfg.Clusters[0].ClusterUsers[0].Password {
			t.Fatalf("user password expected to be %q; got %q", authCfg.Clusters[0].ClusterUsers[0].Password, pass)
		}
	})

	t.Run("auth success", func(t *testing.T) {
		proxy, err := getProxy(authCfg)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		uri := fmt.Sprintf("%s?user=foo&password=bar", fakeServer.URL)
		req := httptest.NewRequest("POST", uri, nil)
		resp := makeCustomRequest(proxy, req)

		expected := "Ok\n"
		if resp != expected {
			t.Fatalf("expected response: %q; got: %q", expected, resp)
		}

		user, pass := getAuth(req)
		if user != authCfg.Clusters[0].ClusterUsers[0].Name {
			t.Fatalf("user name expected to be %q; got %q", authCfg.Clusters[0].ClusterUsers[0].Name, user)
		}

		if pass != authCfg.Clusters[0].ClusterUsers[0].Password {
			t.Fatalf("user password expected to be %q; got %q", authCfg.Clusters[0].ClusterUsers[0].Password, pass)
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
			proxy, err := getProxy(goodCfg)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
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

func getProxy(cfg *config.Config) (*reverseProxy, error) {
	addr, err := url.Parse(fakeServer.URL)
	if err != nil {
		return nil, err
	}

	cfg.Clusters[0].Nodes = []string{addr.Host}
	proxy, err := newConfiguredProxy(cfg)
	if err != nil {
		return nil, err
	}

	return proxy, nil
}

func TestReverseProxy_ServeHTTPConcurrent(t *testing.T) {
	addr, err := url.Parse(fakeServer.URL)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	goodCfg.Clusters[0].Nodes = []string{addr.Host}
	proxy, err := newConfiguredProxy(goodCfg)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	t.Run("parallel requests", func(t *testing.T) {
		f := func() {
			makeRequest(proxy)
		}
		if err := testConcurrent(f, 1000); err != nil {
			t.Fatalf("concurrent test err: %s", err)
		}
	})

	t.Run("parallel requests with config reloading", func(t *testing.T) {
		f := func() {
			go proxy.ApplyConfig(newConfig())
			makeRequest(proxy)
		}
		if err := testConcurrent(f, 100); err != nil {
			t.Fatalf("concurrent test err: %s", err)
		}
	})
}

func newConfig() *config.Config {
	newCfg := *goodCfg
	newCfg.Users = []config.User{
		{
			Name:                 "default",
			MaxConcurrentQueries: rand.Uint32(),
		},
	}

	return &newCfg
}
