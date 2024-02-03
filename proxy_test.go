package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"hash/fnv"
	"io"
	"math/rand"
	"net"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/contentsquare/chproxy/cache"

	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/contentsquare/chproxy/config"
	"github.com/stretchr/testify/assert"
)

var nbHeavyRequestsInflight int64 = 0
var nbRequestsInflight int64 = 0
var totalNbOfRequests uint64 = 0
var shouldStop uint64 = 0

const max_concurrent_goroutines = 256

const heavyRequestDuration = time.Millisecond * 512
const defaultUsername = "default"
const (
	okResponse         = "1"
	badGatewayResponse = "]: cannot reach 127.0.0.1:"
)
const (
	testCacheDir    = "./test-cache-data"
	fileSystemCache = "file_system_cache"
)

var goodCfg = &config.Config{
	Server: config.Server{
		Metrics: config.Metrics{
			Namespace: "proxy_test"},
	},
	Clusters: []config.Cluster{
		{
			Name:   "cluster",
			Scheme: "http",
			Replicas: []config.Replica{
				{
					Nodes: []string{"localhost:8123"},
				},
			},
			ClusterUsers: []config.ClusterUser{
				{
					Name: "web",
				},
			},
		},
	},
	Users: []config.User{
		{
			Name:      defaultUsername,
			ToCluster: "cluster",
			ToUser:    "web",
		},
	},
	ParamGroups: []config.ParamGroup{
		{Name: "param_test", Params: []config.Param{{Key: "param_key", Value: "param_value"}}},
	},
}
var goodCfgWithCache = &config.Config{
	Clusters: []config.Cluster{
		{
			Name:   "cluster",
			Scheme: "http",
			Replicas: []config.Replica{
				{
					Nodes: []string{"localhost:8123"},
				},
			},
			ClusterUsers: []config.ClusterUser{
				{
					Name: "web",
				},
			},
		},
	},
	Users: []config.User{
		{
			Name:      defaultUsername,
			ToCluster: "cluster",
			ToUser:    "web",
			Cache:     fileSystemCache,
		},
	},
	ParamGroups: []config.ParamGroup{
		{Name: "param_test", Params: []config.Param{{Key: "param_key", Value: "param_value"}}},
	},
	Caches: []config.Cache{
		{
			Name: fileSystemCache,
			Mode: "file_system",
			FileSystem: config.FileSystemCacheConfig{
				Dir:     testCacheDir,
				MaxSize: config.ByteSize(1024 * 1024),
			},
			Expire: config.Duration(1000 * 60 * 60),
		},
	},
	MaxErrorReasonSize: config.ByteSize(100 << 20),
}
var goodCfgWithCacheAndMaxErrorReasonSize = &config.Config{
	Clusters: []config.Cluster{
		{
			Name:   "cluster",
			Scheme: "http",
			Replicas: []config.Replica{
				{
					Nodes: []string{"localhost:8123"},
				},
			},
			ClusterUsers: []config.ClusterUser{
				{
					Name: "web",
				},
			},
		},
	},
	Users: []config.User{
		{
			Name:      defaultUsername,
			ToCluster: "cluster",
			ToUser:    "web",
			Cache:     fileSystemCache,
		},
	},
	ParamGroups: []config.ParamGroup{
		{Name: "param_test", Params: []config.Param{{Key: "param_key", Value: "param_value"}}},
	},
	Caches: []config.Cache{
		{
			Name: fileSystemCache,
			Mode: "file_system",
			FileSystem: config.FileSystemCacheConfig{
				Dir:     testCacheDir,
				MaxSize: config.ByteSize(1024 * 1024),
			},
			Expire: config.Duration(1000 * 60 * 60),
		},
	},
}

func newConfiguredProxy(cfg *config.Config) (*reverseProxy, error) {
	p := newReverseProxy(&cfg.ConnectionPool)
	if err := p.applyConfig(cfg); err != nil {
		return p, fmt.Errorf("error while loading config: %s", err)
	}
	return p, nil
}
func init() {
	// we need to initiliaze prometheus metrics
	// otherwise the calls the proxy.applyConfig will fail
	// due to memory issues if someone only runs proxy_test
	registerMetrics(goodCfg)
}

func TestNewReverseProxy(t *testing.T) {
	proxy := newReverseProxy(&goodCfg.ConnectionPool)
	if err := proxy.applyConfig(goodCfg); err != nil {
		t.Fatalf("error while loading config: %s", err)
	}
	if len(proxy.clusters) != 1 {
		t.Fatalf("got %d hosts; expResponse: %d", len(proxy.clusters), 1)
	}
	c := proxy.clusters["cluster"]
	r := c.replicas[0]
	if len(r.hosts) != 1 {
		t.Fatalf("got %d hosts; expResponse: %d", len(r.hosts), 1)
	}
	if r.hosts[0].Host() != "localhost:8123" {
		t.Fatalf("got %s host; expResponse: %s", r.hosts[0].Host(), "localhost:8123")
	}
	if len(proxy.users) != 1 {
		t.Fatalf("got %d users; expResponse: %d", len(proxy.users), 1)
	}
	if _, ok := proxy.users[defaultUsername]; !ok {
		t.Fatalf("expected user %q to be present in users", defaultUsername)
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
					Name: defaultUsername,
				},
			},
		},
	},
	Users: []config.User{
		{
			Name:      defaultUsername,
			ToCluster: "cluster",
			ToUser:    "foo",
		},
	},
}

var badCfgWithNoHeartBeatUser = &config.Config{
	Clusters: []config.Cluster{
		{
			Name:   "badCfgWithNoHeartBeatUser",
			Scheme: "http",
			Nodes:  []string{"localhost:8123"},
			ClusterUsers: []config.ClusterUser{
				{
					Name: defaultUsername,
				},
			},
			HeartBeat: config.HeartBeat{
				Request: "/not_ping",
			},
		},
	},
	Users: []config.User{
		{
			Name:         "analyst_*",
			IsWildcarded: true,
			ToCluster:    "badCfgWithNoHeartBeatUser",
			ToUser:       defaultUsername,
		},
	},
}

func TestApplyConfig(t *testing.T) {
	proxy, err := newConfiguredProxy(goodCfg)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if err = proxy.applyConfig(badCfg); err == nil {
		t.Fatalf("error expected; got nil")
	}
	if _, ok := proxy.clusters["badCfg"]; ok {
		t.Fatalf("bad config applied; expected previous config")
	}
	if err := proxy.applyConfig(badCfgWithNoHeartBeatUser); err == nil {
		t.Fatalf("error expected; got nil")
	} else if err.Error() != "`cluster.heartbeat.user ` cannot be unset for \"badCfgWithNoHeartBeatUser\" because a wildcarded user cannot send heartbeat" {
		t.Fatalf("unexpected error %s", err.Error())
	}
	if _, ok := proxy.clusters["badCfgWithNoHeartBeatUser"]; ok {
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

var wildcardedCfg = &config.Config{
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
				{
					Name: "analyst_*",
				},
				{
					Name: "*-UK",
				},
			},
		},
	},
	Users: []config.User{
		{
			Name:         "analyst_*",
			ToCluster:    "cluster",
			ToUser:       "analyst_*",
			IsWildcarded: true,
		},
		{
			Name:         "*-UK",
			ToCluster:    "cluster",
			ToUser:       "*-UK",
			IsWildcarded: true,
		},
	},
}

var fullWildcardedCfg = &config.Config{
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
				{
					Name: "*",
				},
			},
		},
	},
	Users: []config.User{
		{
			Name:         "*",
			ToCluster:    "cluster",
			ToUser:       "*",
			IsWildcarded: true,
		},
	},
}

func compareTransactionFailReason(t *testing.T, p *reverseProxy, user config.ClusterUser, query string, failReason string) {
	h := fnv.New32a()
	h.Write([]byte(user.Name + user.Password))
	transactionKey := cache.NewKey([]byte(query), url.Values{"query": []string{query}}, "", 0, 0, h.Sum32())
	transactionStatus, err := p.caches[fileSystemCache].TransactionRegistry.Status(transactionKey)
	assert.Nil(t, err)
	assert.Equal(t, failReason, transactionStatus.FailReason)
}

func TestReverseProxy_ServeHTTP1(t *testing.T) {
	query := "SELECT123456"
	testCases := []struct {
		cfg                   *config.Config
		name                  string
		expResponse           string
		expStatusCode         int
		f                     func(p *reverseProxy) *http.Response
		transactionFailReason string
	}{
		{
			cfg:           goodCfg,
			name:          "Bad gatway response without cache",
			expResponse:   badGatewayResponse,
			expStatusCode: http.StatusBadGateway,
			f: func(p *reverseProxy) *http.Response {
				req := httptest.NewRequest("GET", fmt.Sprintf("%s/badGateway?query=%s", fakeServer.URL, query), nil)
				return makeCustomRequest(p, req)
			},
		},
		{
			cfg:           goodCfgWithCache,
			name:          "Bad gatway response with cache",
			expResponse:   badGatewayResponse,
			expStatusCode: http.StatusBadGateway,
			f: func(p *reverseProxy) *http.Response {
				req := httptest.NewRequest("GET", fmt.Sprintf("%s/badGateway?query=%s", fakeServer.URL, query), nil)
				// cleaning the cache to be sure it will be a cache miss although the query isn't supposed to be cached
				os.RemoveAll(testCacheDir)
				return makeCustomRequest(p, req)
			},
			transactionFailReason: "[concurrent query failed] ]: cannot reach 127.0.0.1:\n",
		},
		{
			cfg:           goodCfg,
			name:          "Ok response",
			expResponse:   okResponse,
			expStatusCode: http.StatusOK,
			f:             func(p *reverseProxy) *http.Response { return makeRequest(p) },
		},
		{
			cfg:           goodCfg,
			name:          "max concurrent queries for cluster user",
			expResponse:   "limits for cluster user \"web\" are exceeded: max_concurrent_queries limit: 1;",
			expStatusCode: http.StatusTooManyRequests,
			f: func(p *reverseProxy) *http.Response {
				p.clusters["cluster"].users["web"].maxConcurrentQueries = 1
				runHeavyRequestInGoroutine(p, 1, true)
				return makeRequest(p)
			},
		},
		{
			cfg:           goodCfg,
			name:          "max time for cluster user",
			expResponse:   "timeout for cluster user \"web\" exceeded: 10ms",
			expStatusCode: http.StatusGatewayTimeout,
			f: func(p *reverseProxy) *http.Response {
				p.clusters["cluster"].users["web"].maxExecutionTime = time.Millisecond * 10
				return makeHeavyRequest(p, time.Millisecond*20)
			},
		},
		{
			cfg:           goodCfg,
			name:          "choose max time between users",
			expResponse:   fmt.Sprintf("timeout for user \"%s\" exceeded: 10ms", defaultUsername),
			expStatusCode: http.StatusGatewayTimeout,
			f: func(p *reverseProxy) *http.Response {
				p.users[defaultUsername].maxExecutionTime = time.Millisecond * 10
				p.clusters["cluster"].users["web"].maxExecutionTime = time.Millisecond * 15
				return makeHeavyRequest(p, time.Millisecond*20)
			},
		},
		{
			cfg:           goodCfg,
			name:          "choose max time between users2",
			expResponse:   "timeout for cluster user \"web\" exceeded: 10ms",
			expStatusCode: http.StatusGatewayTimeout,
			f: func(p *reverseProxy) *http.Response {
				p.users[defaultUsername].maxExecutionTime = time.Millisecond * 15
				p.clusters["cluster"].users["web"].maxExecutionTime = time.Millisecond * 10
				return makeHeavyRequest(p, time.Millisecond*20)
			},
		},
		{
			cfg:           goodCfg,
			name:          "max concurrent queries for user",
			expResponse:   fmt.Sprintf("limits for user \"%s\" are exceeded: max_concurrent_queries limit: 1;", defaultUsername),
			expStatusCode: http.StatusTooManyRequests,
			f: func(p *reverseProxy) *http.Response {
				p.users[defaultUsername].maxConcurrentQueries = 1
				runHeavyRequestInGoroutine(p, 1, true)
				return makeRequest(p)
			},
		},
		{
			cfg:           goodCfg,
			name:          "queuing queries for user",
			expResponse:   okResponse,
			expStatusCode: http.StatusOK,
			f: func(p *reverseProxy) *http.Response {
				p.users[defaultUsername].maxConcurrentQueries = 1
				p.users[defaultUsername].queueCh = make(chan struct{}, 2)
				runHeavyRequestInGoroutine(p, 1, true)
				return makeRequest(p)
			},
		},
		{
			cfg:           goodCfg,
			name:          "queuing queries for cluster user",
			expResponse:   okResponse,
			expStatusCode: http.StatusOK,
			f: func(p *reverseProxy) *http.Response {
				p.users[defaultUsername].maxConcurrentQueries = 1
				p.clusters["cluster"].users["web"].queueCh = make(chan struct{}, 2)
				runHeavyRequestInGoroutine(p, 1, true)
				return makeRequest(p)
			},
		},
		{
			cfg:           goodCfg,
			name:          "max payload size limit",
			expResponse:   okResponse,
			expStatusCode: http.StatusOK,
			f: func(p *reverseProxy) *http.Response {
				p.caches["max_payload_size"] = &cache.AsyncCache{
					MaxPayloadSize: 8 * 1024 * 1024,
				}
				p.users[defaultUsername].cache = p.caches["max_payload_size"]
				return makeRequest(p)
			},
		},
		{
			cfg:           goodCfg,
			name:          "queue overflow for user",
			expResponse:   fmt.Sprintf("limits for user \"%s\" are exceeded: max_concurrent_queries limit: 1", defaultUsername),
			expStatusCode: http.StatusTooManyRequests,
			f: func(p *reverseProxy) *http.Response {
				p.users[defaultUsername].maxConcurrentQueries = 1
				p.users[defaultUsername].queueCh = make(chan struct{}, 1)
				// we don't wait the requests to be handled by the fakeServer because one of them will be enqueued and not handled
				// this is why we handle this part manually
				nbRequest := atomic.LoadUint64(&totalNbOfRequests)
				runHeavyRequestInGoroutine(p, 2, false)
				counter := 0
				for atomic.LoadUint64(&totalNbOfRequests) < nbRequest+2 && counter < 200 {
					time.Sleep(1 * time.Millisecond)
					counter++
				}
				return makeRequest(p)
			},
		},
		{
			cfg:           authCfg,
			name:          "disallow https",
			expResponse:   "user \"foo\" is not allowed to access via https",
			expStatusCode: http.StatusForbidden,
			f: func(p *reverseProxy) *http.Response {
				p.users["foo"].denyHTTPS = true
				req := httptest.NewRequest("POST", fakeServer.URL, nil)
				req.SetBasicAuth("foo", "bar")
				req.TLS = &tls.ConnectionState{
					Version:           tls.VersionTLS12,
					HandshakeComplete: true,
				}
				return makeCustomRequest(p, req)
			},
		},
		{
			cfg:           authCfg,
			name:          "basic auth ok",
			expResponse:   okResponse,
			expStatusCode: http.StatusOK,
			f: func(p *reverseProxy) *http.Response {
				req := httptest.NewRequest("POST", fakeServer.URL, nil)
				req.SetBasicAuth("foo", "bar")
				return makeCustomRequest(p, req)
			},
		},
		{
			cfg:           goodCfg,
			name:          "disallow http",
			expResponse:   fmt.Sprintf("user \"%s\" is not allowed to access via http", defaultUsername),
			expStatusCode: http.StatusForbidden,
			f: func(p *reverseProxy) *http.Response {
				p.users[defaultUsername].denyHTTP = true
				return makeRequest(p)
			},
		},
		{
			cfg:           authCfg,
			name:          "basic auth wrong name",
			expResponse:   "invalid username or password for user \"fooo\"",
			expStatusCode: http.StatusUnauthorized,
			f: func(p *reverseProxy) *http.Response {
				req := httptest.NewRequest("POST", fakeServer.URL, nil)
				req.SetBasicAuth("fooo", "bar")
				return makeCustomRequest(p, req)
			},
		},
		{
			cfg:           authCfg,
			name:          "basic auth wrong pass",
			expResponse:   "invalid username or password for user \"foo\"",
			expStatusCode: http.StatusUnauthorized,
			f: func(p *reverseProxy) *http.Response {
				req := httptest.NewRequest("POST", fakeServer.URL, nil)
				req.SetBasicAuth("foo", "baar")
				return makeCustomRequest(p, req)
			},
		},
		{
			cfg:           authCfg,
			name:          "auth ok",
			expResponse:   okResponse,
			expStatusCode: http.StatusOK,
			f: func(p *reverseProxy) *http.Response {
				uri := fmt.Sprintf("%s?user=foo&password=bar", fakeServer.URL)
				req := httptest.NewRequest("POST", uri, nil)
				return makeCustomRequest(p, req)
			},
		},
		{
			cfg:           authCfg,
			name:          "auth wrong name",
			expResponse:   "invalid username or password for user \"fooo\"",
			expStatusCode: http.StatusUnauthorized,
			f: func(p *reverseProxy) *http.Response {
				uri := fmt.Sprintf("%s?user=fooo&password=bar", fakeServer.URL)
				req := httptest.NewRequest("POST", uri, nil)
				return makeCustomRequest(p, req)
			},
		},
		{
			cfg:           authCfg,
			name:          "auth wrong name",
			expResponse:   "invalid username or password for user \"foo\"",
			expStatusCode: http.StatusUnauthorized,
			f: func(p *reverseProxy) *http.Response {
				uri := fmt.Sprintf("%s?user=foo&password=baar", fakeServer.URL)
				req := httptest.NewRequest("POST", uri, nil)
				return makeCustomRequest(p, req)
			},
		},
		{
			cfg:           authCfg,
			name:          "headers auth ok",
			expResponse:   okResponse,
			expStatusCode: http.StatusOK,
			f: func(p *reverseProxy) *http.Response {
				req := httptest.NewRequest("POST", fakeServer.URL, nil)
				req.Header.Set("X-ClickHouse-User", "foo")
				req.Header.Set("X-ClickHouse-Key", "bar")
				return makeCustomRequest(p, req)
			},
		},
		{
			cfg:           authCfg,
			name:          "header auth wrong name",
			expResponse:   "invalid username or password for user \"fooo\"",
			expStatusCode: http.StatusUnauthorized,
			f: func(p *reverseProxy) *http.Response {
				req := httptest.NewRequest("POST", fakeServer.URL, nil)
				req.Header.Set("X-ClickHouse-User", "fooo")
				req.Header.Set("X-ClickHouse-Key", "bar")
				return makeCustomRequest(p, req)
			},
		},
		{
			cfg:           authCfg,
			name:          "header auth wrong name",
			expResponse:   "invalid username or password for user \"foo\"",
			expStatusCode: http.StatusUnauthorized,
			f: func(p *reverseProxy) *http.Response {
				req := httptest.NewRequest("POST", fakeServer.URL, nil)
				req.Header.Set("X-ClickHouse-User", "foo")
				req.Header.Set("X-ClickHouse-Key", "baar")
				return makeCustomRequest(p, req)
			},
		},
		{
			cfg:           authCfg,
			name:          "post request max payload size",
			expResponse:   okResponse,
			expStatusCode: http.StatusOK,
			f: func(p *reverseProxy) *http.Response {
				uri := fmt.Sprintf("%s?user=foo&password=bar", fakeServer.URL)
				req := httptest.NewRequest("POST", uri, nil)
				p.caches["max_payload_size"] = &cache.AsyncCache{
					MaxPayloadSize: 8 * 1024 * 1024,
				}
				p.users["foo"].cache = p.caches["max_payload_size"]
				return makeCustomRequest(p, req)
			},
		},
		{
			cfg:           wildcardedCfg,
			name:          "wildcarded Ok1",
			expResponse:   "user: analyst_jane, password: jane_pass\n" + okResponse,
			expStatusCode: http.StatusOK,
			f: func(p *reverseProxy) *http.Response {
				req := httptest.NewRequest("POST", fakeServer.URL, nil)
				req.Header.Set("X-ClickHouse-User", "analyst_jane")
				req.Header.Set("X-ClickHouse-Key", "jane_pass")
				return makeCustomRequest(p, req)
			},
		},
		{
			cfg:           wildcardedCfg,
			name:          "wildcarded Ok2",
			expResponse:   "user: john-UK, password: john_pass\n" + okResponse,
			expStatusCode: http.StatusOK,
			f: func(p *reverseProxy) *http.Response {
				req := httptest.NewRequest("POST", fakeServer.URL, nil)
				req.Header.Set("X-ClickHouse-User", "john-UK")
				req.Header.Set("X-ClickHouse-Key", "john_pass")
				return makeCustomRequest(p, req)
			},
		},
		{
			cfg:           fullWildcardedCfg,
			name:          "wildcarded Ok3",
			expResponse:   "user: toto, password: titi\n" + okResponse,
			expStatusCode: http.StatusOK,
			f: func(p *reverseProxy) *http.Response {
				req := httptest.NewRequest("POST", fakeServer.URL, nil)
				req.Header.Set("X-ClickHouse-User", "toto")
				req.Header.Set("X-ClickHouse-Key", "titi")
				return makeCustomRequest(p, req)
			},
		},
		{
			cfg:           fullWildcardedCfg,
			name:          "wildcarded Ko for default user",
			expResponse:   fmt.Sprintf("invalid username or password for user \"%s\"", defaultUsername),
			expStatusCode: http.StatusUnauthorized,
			f: func(p *reverseProxy) *http.Response {
				req := httptest.NewRequest("POST", fakeServer.URL, nil)
				req.Header.Set("X-ClickHouse-User", defaultUsername)
				req.Header.Set("X-ClickHouse-Key", "")
				return makeCustomRequest(p, req)
			},
		},
		{
			cfg:           goodCfg,
			name:          "request packet size token limit for user",
			expResponse:   "limits for user \"default\" is exceeded: request_packet_size_tokens_burst limit: 4",
			expStatusCode: http.StatusTooManyRequests,
			f: func(p *reverseProxy) *http.Response {
				p.users[defaultUsername].reqPacketSizeTokensBurst = 4
				p.users[defaultUsername].reqPacketSizeTokenLimiter = rate.NewLimiter(
					rate.Limit(1), 4)
				go makeHeavyRequest(p, time.Millisecond*20)
				return makeHeavyRequest(p, time.Millisecond*200)
			},
		},
		{
			cfg:           goodCfg,
			name:          "request packet size token limit for cluster user",
			expResponse:   "limits for cluster user \"web\" is exceeded: request_packet_size_tokens_burst limit: 4",
			expStatusCode: http.StatusTooManyRequests,
			f: func(p *reverseProxy) *http.Response {
				p.clusters["cluster"].users["web"].reqPacketSizeTokensBurst = 4
				p.clusters["cluster"].users["web"].reqPacketSizeTokenLimiter = rate.NewLimiter(
					rate.Limit(1), 4)
				go makeHeavyRequest(p, time.Millisecond*20)
				return makeHeavyRequest(p, time.Millisecond*200)
			},
		},
		{
			cfg:           goodCfg,
			name:          "queuing request packet size token limit for user",
			expResponse:   okResponse,
			expStatusCode: http.StatusOK,
			f: func(p *reverseProxy) *http.Response {
				p.users[defaultUsername].reqPacketSizeTokensBurst = 5
				p.users[defaultUsername].reqPacketSizeTokenLimiter = rate.NewLimiter(
					rate.Limit(1), 5)
				p.users[defaultUsername].queueCh = make(chan struct{}, 2)
				p.users[defaultUsername].maxQueueTime = 10 * time.Second
				runHeavyRequestInGoroutine(p, 1, true)
				return makeHeavyRequest(p, time.Millisecond*200)
			},
		},
		{
			cfg:           goodCfg,
			name:          "queuing request with packet size token limit for cluster user",
			expResponse:   okResponse,
			expStatusCode: http.StatusOK,
			f: func(p *reverseProxy) *http.Response {
				p.clusters["cluster"].users["web"].reqPacketSizeTokensBurst = 5
				p.clusters["cluster"].users["web"].reqPacketSizeTokenLimiter = rate.NewLimiter(
					rate.Limit(1), 5)
				p.clusters["cluster"].users["web"].queueCh = make(chan struct{}, 2)
				p.clusters["cluster"].users["web"].maxQueueTime = 10 * time.Second
				runHeavyRequestInGoroutine(p, 1, true)
				return makeHeavyRequest(p, time.Millisecond*200)
			},
		},
		{
			cfg:           goodCfgWithCacheAndMaxErrorReasonSize,
			name:          "max error reason size",
			expResponse:   badGatewayResponse,
			expStatusCode: http.StatusBadGateway,
			f: func(p *reverseProxy) *http.Response {
				req := httptest.NewRequest("GET", fmt.Sprintf("%s/badGateway?query=%s", fakeServer.URL, query), nil)
				return makeCustomRequest(p, req)
			},
			transactionFailReason: "unknown error reason",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stopAllRequestsInFlight()
			proxy, err := getProxy(tc.cfg)
			if err != nil {
				t.Fatalf("unexpected error: %s for %q", err, tc.name)
			}
			resp := tc.f(proxy)
			b := bbToString(t, resp.Body)
			resp.Body.Close()
			if len(tc.cfg.Caches) != 0 {
				compareTransactionFailReason(t, proxy, tc.cfg.Clusters[0].ClusterUsers[0], query, tc.transactionFailReason)
			}
			if !strings.Contains(b, tc.expResponse) {
				t.Fatalf("expected response: %q; got: %q for %q", tc.expResponse, b, tc.name)
			}
			if tc.expStatusCode != resp.StatusCode {
				t.Fatalf("unexpected status code: %d; expected: %d for %q", resp.StatusCode, tc.expStatusCode, tc.name)
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

		expected := okResponse
		b := bbToString(t, resp.Body)
		if !strings.Contains(b, expected) {
			t.Fatalf("expected response: %q; got: %q", expected, b)
		}
		resp.Body.Close()

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

		expected := okResponse
		b := bbToString(t, resp.Body)
		if !strings.Contains(b, expected) {
			t.Fatalf("expected response: %q; got: %q", expected, b)
		}
		resp.Body.Close()

		user, pass := getAuth(req)
		if user != authCfg.Clusters[0].ClusterUsers[0].Name {
			t.Fatalf("user name expected to be %q; got %q", authCfg.Clusters[0].ClusterUsers[0].Name, user)
		}
		if pass != authCfg.Clusters[0].ClusterUsers[0].Password {
			t.Fatalf("user password expected to be %q; got %q", authCfg.Clusters[0].ClusterUsers[0].Password, pass)
		}
	})
}

func TestKillQuery(t *testing.T) {
	testCases := []struct {
		name string
		f    func(p *reverseProxy) *http.Response
	}{
		{
			name: "timeout user",
			f: func(p *reverseProxy) *http.Response {
				p.users[defaultUsername].maxExecutionTime = time.Millisecond * 5
				return makeHeavyRequest(p, time.Millisecond*40)
			},
		},
		{
			name: "timeout cluster user",
			f: func(p *reverseProxy) *http.Response {
				p.clusters["cluster"].users["web"].maxExecutionTime = time.Millisecond * 5
				return makeHeavyRequest(p, time.Millisecond*40)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			proxy, err := getProxy(goodCfg)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			resp := tc.f(proxy)
			b := bbToString(t, resp.Body)
			id := extractID(scopeIDRe, b)
			if len(id) == 0 {
				t.Fatalf("expected Id to be extracted from %q", b)
			}
			// waiting btw 5 and 200 msec to get the answer from CHProxy
			// because this code on github is unstable due to the poor performances
			// of the server running the CI.
			loop := true
			counter := 0
			for loop {
				time.Sleep(time.Millisecond * 5)
				counter++

				_, err := registry.get(id)
				if err == nil || counter > 40 {
					loop = false
				}
			}

			state, err := registry.get(id)
			if err != nil {
				t.Fatalf("unexpected requestRegistry err for key %q: %s", id, err)
			}
			if !state {
				t.Fatalf("query expected to be killed; response: %s", b)
			}
		})
	}
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
			req := httptest.NewRequest("GET", fmt.Sprintf("%s/fast", fakeServer.URL), nil)
			makeCustomRequest(proxy, req)
		}
		if err := testConcurrent(f, 500); err != nil {
			t.Fatalf("concurrent test err: %s", err)
		}
	})
	t.Run("parallel requests with config reloading", func(t *testing.T) {
		f := func() {
			proxy.applyConfig(newConfig())
			req := httptest.NewRequest("GET", fmt.Sprintf("%s/fast", fakeServer.URL), nil)
			makeCustomRequest(proxy, req)
		}
		if err := testConcurrent(f, 100); err != nil {
			t.Fatalf("concurrent test err: %s", err)
		}
	})
}

func TestReverseProxy_ServeHTTP2(t *testing.T) {
	testCases := []struct {
		name            string
		allowedNetworks config.Networks
		expResponse     string
	}{
		{
			name:            "empty allowed networks",
			allowedNetworks: config.Networks{},
		},
		{
			name:            "allow addr",
			allowedNetworks: config.Networks{getNetwork("192.0.2.1")},
		},
		{
			name:            "allow addr by mask",
			allowedNetworks: config.Networks{getNetwork("192.0.2.1/32")},
		},
	}

	f := func(cfg *config.Config) {
		proxy, err := getProxy(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		resp := makeRequest(proxy)
		b := bbToString(t, resp.Body)
		resp.Body.Close()
		if !strings.Contains(b, okResponse) {
			t.Fatalf("expected response: %q; got: %q", okResponse, b)
		}
	}

	for _, tc := range testCases {
		t.Run("user "+tc.name, func(t *testing.T) {
			goodCfg.Users[0].AllowedNetworks = tc.allowedNetworks
			f(goodCfg)
		})
		t.Run("cluster user "+tc.name, func(t *testing.T) {
			goodCfg.Clusters[0].ClusterUsers[0].AllowedNetworks = tc.allowedNetworks
			f(goodCfg)
		})
	}

	t.Run("cluster user disallow addr", func(t *testing.T) {
		goodCfg.Clusters[0].ClusterUsers[0].AllowedNetworks = config.Networks{getNetwork("192.0.2.2/32"), getNetwork("192.0.2.2")}
		proxy, err := getProxy(goodCfg)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		resp := makeRequest(proxy)
		expected := "cluster user \"web\" is not allowed to access"
		b := bbToString(t, resp.Body)
		resp.Body.Close()
		if !strings.Contains(b, expected) {
			t.Fatalf("expected response: %q; got: %q", expected, b)
		}
	})

	t.Run("user disallow addr", func(t *testing.T) {
		goodCfg.Users[0].AllowedNetworks = config.Networks{getNetwork("192.0.2.2/32"), getNetwork("192.0.2.2")}
		proxy, err := getProxy(goodCfg)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		resp := makeRequest(proxy)
		expected := fmt.Sprintf("user \"%s\" is not allowed to access", defaultUsername)
		b := bbToString(t, resp.Body)
		resp.Body.Close()
		if !strings.Contains(b, expected) {
			t.Fatalf("expected response: %q; got: %q", expected, b)
		}
	})

	t.Run("request body not empty", func(t *testing.T) {
		proxy, err := getProxy(goodCfg)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		body := bytes.NewBufferString("SELECT sleep(1.5)")
		expected := "SELECT sleep(1.5)"
		req := httptest.NewRequest("POST", fakeServer.URL, body)

		resp := makeCustomRequest(proxy, req)
		b := bbToString(t, resp.Body)
		resp.Body.Close()

		if !strings.Contains(b, expected) {
			t.Fatalf("expected response: %q; got: %q", expected, b)
		}

	})
}

func getNetwork(s string) *net.IPNet {
	if !strings.Contains(s, `/`) {
		s += "/32"
	}
	_, ipnet, _ := net.ParseCIDR(s)
	return ipnet
}

const killQueryPattern = "KILL QUERY WHERE query_id"

var (
	// quick fix until the test file is refactored.
	// Many tests rely on concurrency of goroutines.
	// But, because of the use of sleep, only a few goroutine can run in concurrence and the other are blocked
	// cf: https://stackoverflow.com/questions/62527705/will-time-sleep-block-goroutine
	// Because of that, on most tests the goroutines are running sequencially
	// This id why we increase the number concurrent goroutines (the default number is the number of cpu of the computer running the tests, which is quite low on github actions)
	// we must increase this number before the instanciation of the fakeServer, otherwise it's not taken into account during the tests
	_        = increaseMaxConccurentGoroutine()
	registry = newRequestRegistry()
	handler  = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&nbRequestsInflight, 1)
		defer atomic.AddInt64(&nbRequestsInflight, -1)
		if r.URL.Path == "/fast" {
			fmt.Fprintln(w, okResponse)
			return
		}
		if r.URL.Path == "/badGateway" {
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintln(w, badGatewayResponse)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			panic(err)
		}
		b := string(body)
		r.Body.Close()

		if n, p, found := r.BasicAuth(); found {
			fmt.Fprintf(w, "user: %s, password: %s\n", n, p)
		}

		qid := r.URL.Query().Get("query_id")
		if len(qid) == 0 && len(b) == 0 {
			// it's could be a health-check
			fmt.Fprintln(w, okResponse)
			return
		}
		registry.set(qid, false)
		if len(b) != 0 {
			if strings.Contains(b, killQueryPattern) {
				qid = extractID(queryIDRe, b)
				registry.set(qid, true)
			} else {
				d, err := time.ParseDuration(b)
				if err != nil {
					panic(fmt.Sprintf("error while imitating delay at fakeServer handler: %s", err))
				}

				// at this step the query is really processed by the server
				// we're only interested by long queries, i.e the one taking more than 0 msec
				if d == heavyRequestDuration {
					atomic.AddInt64(&nbHeavyRequestsInflight, 1)
				}

				// instead of sleeping for the whole duration, we sleep msec per msec so that the query can be
				// cancel once it's associated test is over because this test suite generate a lot of goroutines,
				// which can be an issue on small cpu like github actions
				for i := int64(0); i < d.Milliseconds() && atomic.LoadUint64(&shouldStop) == 0; i++ {
					time.Sleep(time.Millisecond)
				}
			}
		}

		fmt.Fprintln(w, okResponse)
	})

	fakeServer = httptest.NewServer(handler)
)

func makeRequest(p *reverseProxy) *http.Response { return makeHeavyRequest(p, time.Duration(0)) }

func makeHeavyRequest(p *reverseProxy, duration time.Duration) *http.Response {
	body := bytes.NewBufferString(duration.String())
	req := httptest.NewRequest("POST", fakeServer.URL, body)
	return makeCustomRequest(p, req)
}

type testCloseNotifier struct {
	http.ResponseWriter
}

func (tcn *testCloseNotifier) CloseNotify() <-chan bool {
	return make(chan bool)
}

func makeCustomRequest(p *reverseProxy, req *http.Request) *http.Response {
	atomic.AddUint64(&totalNbOfRequests, 1)
	rw := httptest.NewRecorder()
	cn := &testCloseNotifier{rw}
	p.ServeHTTP(cn, req)
	return rw.Result()
}

func getProxy(c *config.Config) (*reverseProxy, error) {
	addr, err := url.Parse(fakeServer.URL)
	if err != nil {
		return nil, err
	}
	cfg := *c
	cfg.Clusters = make([]config.Cluster, len(c.Clusters))
	copy(cfg.Clusters, c.Clusters)
	cfg.Clusters[0].Nodes = []string{addr.Host}
	proxy, err := newConfiguredProxy(&cfg)
	if err != nil {
		return nil, err
	}

	return proxy, nil
}

func newConfig() *config.Config {
	newCfg := *goodCfg
	newCfg.Users = []config.User{
		{
			Name:                 defaultUsername,
			MaxConcurrentQueries: rand.Uint32(),
		},
	}
	return &newCfg
}

func bbToString(t *testing.T, r io.Reader) string {
	response, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("unexpected err while reading: %s", err)
	}
	return string(response)
}

var (
	scopeIDRe = regexp.MustCompile("Id: (.*?);")
	queryIDRe = regexp.MustCompile("'(.*?)'")
)

func extractID(re *regexp.Regexp, s string) string {
	subm := re.FindStringSubmatch(s)
	if len(subm) < 2 {
		return ""
	}
	return subm[1]
}

type requestRegistry struct {
	sync.Mutex
	// r is a registry of requests, where key is a query_id
	// and value is a state - was query killed(true) or not(false)
	r map[string]bool
}

func increaseMaxConccurentGoroutine() int {
	nb := runtime.GOMAXPROCS(max_concurrent_goroutines)
	return nb
}
func newRequestRegistry() *requestRegistry {
	return &requestRegistry{
		r: make(map[string]bool),
	}
}
func (r *requestRegistry) set(key string, v bool) {
	r.Lock()
	r.r[key] = v
	r.Unlock()
}

func (r *requestRegistry) get(key string) (bool, error) {
	r.Lock()
	defer r.Unlock()
	v, ok := r.r[key]
	if !ok {
		return false, fmt.Errorf("no such key")
	}
	return v, nil
}

func TestCalcQueryParamsHash(t *testing.T) {
	testCases := []struct {
		name           string
		input          url.Values
		expectedResult uint32
	}{
		{
			"nil Value",
			nil,
			0,
		},
		{
			"empty calcQueryParamsHash",
			url.Values{},
			0,
		},
		{
			"map with non param_ value",
			url.Values{"session_id": {"foo", "bar"}},
			0,
		},
		{
			"map with only param_ value",
			url.Values{"param_limit": {"1"}},
			0x94a386,
		},
		{
			"map with only param_ value. value affects result",
			url.Values{"param_limit": {"2"}},
			0x329bae01,
		},
		{
			"map with mix of param_ and non-param_ value",
			url.Values{"param_limit": {"1"}, "session_id": {"foo", "bar"}},
			0x94a386,
		},
		{
			"map with multiple param_ values",
			url.Values{"param_limit": {"1", "2"}, "param_table": {"foo"}},
			0x3a8a5c31,
		},
		{
			"map with multiple param_ values and only first value in array affects result",
			url.Values{"param_limit": {"1"}, "param_table": {"foo", "bar"}},
			0x3a8a5c31,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := calcQueryParamsHash(tc.input)
			assert.Equal(t, r, tc.expectedResult)
		})
	}
}

// this function runs an number of heavyRequests using goroutines
// then waits that all the requests are currently handled by the fakeServer
func runHeavyRequestInGoroutine(p *reverseProxy, nbHeavyRequest int64, shouldWait bool) {
	atomic.StoreInt64(&nbHeavyRequestsInflight, 0)
	for i := 0; i < int(nbHeavyRequest); i++ {
		go makeHeavyRequest(p, heavyRequestDuration)
	}
	counter := 0
	//we wait up to 200 msec for the requests to be handled by the fakeServer because
	// the code on github actions can be very slow
	for shouldWait && atomic.LoadInt64(&nbHeavyRequestsInflight) < nbHeavyRequest && counter < 200 {
		time.Sleep(time.Millisecond)
		counter++
	}
}

func stopAllRequestsInFlight() {
	atomic.StoreUint64(&shouldStop, 1)
	for atomic.LoadInt64(&nbRequestsInflight) > 0 {
		time.Sleep(time.Millisecond)
	}
	atomic.StoreUint64(&shouldStop, 0)
}
