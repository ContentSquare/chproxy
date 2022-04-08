package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/contentsquare/chproxy/cache"

	"github.com/alicebob/miniredis/v2"
	"github.com/contentsquare/chproxy/config"
	"github.com/contentsquare/chproxy/log"
)

var testDir = "./temp-test-data"

func TestMain(m *testing.M) {
	log.SuppressOutput(true)
	retCode := m.Run()
	log.SuppressOutput(false)
	if err := os.RemoveAll(testDir); err != nil {
		log.Fatalf("cannot remove %q: %s", testDir, err)
	}
	if redisClient != nil {
		redisClient.Close()
	}
	os.Exit(retCode)
}

var redisClient *miniredis.Miniredis

func TestServe(t *testing.T) {
	expectedOkResp := "Ok.\n"
	var testCases = []struct {
		name     string
		file     string
		testFn   func(t *testing.T)
		listenFn func() (net.Listener, chan struct{})
	}{
		{
			"https request",
			"testdata/https.yml",
			func(t *testing.T) {
				req, err := http.NewRequest("GET", "https://127.0.0.1:8443?query=asd", nil)
				checkErr(t, err)
				resp, err := tlsClient.Do(req)
				checkErr(t, err)
				if resp.StatusCode != http.StatusUnauthorized {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusUnauthorized)
				}
				resp.Body.Close()

				req, err = http.NewRequest("GET", "https://127.0.0.1:8443?query=asd", nil)
				checkErr(t, err)
				req.SetBasicAuth("default", "qwerty")
				resp, err = tlsClient.Do(req)
				checkErr(t, err)
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
				}
				resp.Body.Close()
			},
			startTLS,
		},
		{
			"https cache",
			"testdata/https.cache.yml",
			func(t *testing.T) {
				// do request which response must be cached
				q := "SELECT 123"
				req, err := http.NewRequest("GET", "https://127.0.0.1:8443?query="+url.QueryEscape(q), nil)
				checkErr(t, err)
				req.SetBasicAuth("default", "qwerty")
				resp, err := tlsClient.Do(req)
				checkErr(t, err)
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
				}
				checkResponse(t, resp.Body, expectedOkResp)

				// check cached response
				key := &cache.Key{
					Query:          []byte(q),
					AcceptEncoding: "gzip",
					Version:        cache.Version,
				}
				path := fmt.Sprintf("%s/cache/%s", testDir, key.String())
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("err while getting file %q info: %s", path, err)
				}
				rw := httptest.NewRecorder()
				cc := proxy.caches["https_cache"]

				cachedData, err := cc.Get(key)

				if err != nil {
					t.Fatalf("unexpected error while getting response from cache: %s", err)
				}
				err = RespondWithData(rw, cachedData.Data, cachedData.ContentMetadata, cachedData.Ttl, 200)
				if err != nil {
					t.Fatalf("unexpected error while getting response from cache: %s", err)
				}
				checkResponse(t, rw.Body, expectedOkResp)
			},
			startTLS,
		},
		{
			"https cache with mix query source",
			"testdata/https.cache.yml",
			func(t *testing.T) {
				// do request which response must be cached
				queryURLParam := "SELECT * FROM system.numbers"
				queryBody := "LIMIT 10"
				expectedQuery := queryURLParam + "\n" + queryBody
				buf := bytes.NewBufferString(queryBody)
				req, err := http.NewRequest("GET", "https://127.0.0.1:8443?query="+url.QueryEscape(queryURLParam), buf)
				checkErr(t, err)
				req.SetBasicAuth("default", "qwerty")
				resp, err := tlsClient.Do(req)
				checkErr(t, err)
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
				}
				resp.Body.Close()

				// check cached response
				key := &cache.Key{
					Query:          []byte(expectedQuery),
					AcceptEncoding: "gzip",
					Version:        cache.Version,
				}
				path := fmt.Sprintf("%s/cache/%s", testDir, key.String())
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("err while getting file %q info: %s", path, err)
				}
				rw := httptest.NewRecorder()
				cc := proxy.caches["https_cache"]

				cachedData, err := cc.Get(key)

				if err != nil {
					t.Fatalf("unexpected error while writing reposnse from cache: %s", err)
				}

				err = RespondWithData(rw, cachedData.Data, cachedData.ContentMetadata, cachedData.Ttl, 200)
				if err != nil {
					t.Fatalf("unexpected error while getting response from cache: %s", err)
				}

				expected := expectedOkResp
				checkResponse(t, rw.Body, expected)
			},
			startTLS,
		},
		{
			"bad https cache",
			"testdata/https.cache.yml",
			func(t *testing.T) {
				// do request which cause an error
				q := "SELECT ERROR"
				req, err := http.NewRequest("GET", "https://127.0.0.1:8443?query="+url.QueryEscape(q), nil)
				checkErr(t, err)
				req.SetBasicAuth("default", "qwerty")
				resp, err := tlsClient.Do(req)
				checkErr(t, err)
				if resp.StatusCode != http.StatusTeapot {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusTeapot)
				}
				resp.Body.Close()

				// check cached response
				key := &cache.Key{
					Query:          []byte(q),
					AcceptEncoding: "gzip",
					Version:        cache.Version,
				}
				path := fmt.Sprintf("%s/cache/%s", testDir, key.String())
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("err while getting file %q info: %s", path, err)
				}

				// check refreshCacheMetrics()
				req, err = http.NewRequest("GET", "https://127.0.0.1:8443/metrics", nil)
				checkErr(t, err)
				resp, err = tlsClient.Do(req)
				if err != nil {
					t.Fatalf("unexpected error while doing request: %s", err)
				}
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
				}
				resp.Body.Close()
			},
			startTLS,
		},
		{
			"deny https",
			"testdata/https.deny.https.yml",
			func(t *testing.T) {
				req, err := http.NewRequest("GET", "https://127.0.0.1:8443?query=asd", nil)
				checkErr(t, err)
				req.SetBasicAuth("default", "qwerty")
				resp, err := tlsClient.Do(req)
				checkErr(t, err)
				if resp.StatusCode != http.StatusForbidden {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
				}
				expected := "user \"default\" is not allowed to access via https"
				checkResponse(t, resp.Body, expected)
				resp.Body.Close()
			},
			startTLS,
		},
		{
			"https networks",
			"testdata/https.networks.yml",
			func(t *testing.T) {
				req, err := http.NewRequest("GET", "https://127.0.0.1:8443?query=asd", nil)
				checkErr(t, err)
				req.SetBasicAuth("default", "qwerty")
				resp, err := tlsClient.Do(req)
				checkErr(t, err)
				if resp.StatusCode != http.StatusForbidden {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
				}
				expected := "https connections are not allowed from 127.0.0.1"
				checkResponse(t, resp.Body, expected)
				resp.Body.Close()
			},
			startTLS,
		},
		{
			"https user networks",
			"testdata/https.user.networks.yml",
			func(t *testing.T) {
				req, err := http.NewRequest("GET", "https://127.0.0.1:8443?query=asd", nil)
				checkErr(t, err)
				req.SetBasicAuth("default", "qwerty")
				resp, err := tlsClient.Do(req)
				checkErr(t, err)
				if resp.StatusCode != http.StatusForbidden {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
				}
				expected := "user \"default\" is not allowed to access"
				checkResponse(t, resp.Body, expected)
				resp.Body.Close()
			},
			startTLS,
		},
		{
			"https cluster user networks",
			"testdata/https.cluster.user.networks.yml",
			func(t *testing.T) {
				req, err := http.NewRequest("GET", "https://127.0.0.1:8443?query=asd", nil)
				checkErr(t, err)
				req.SetBasicAuth("default", "qwerty")
				resp, err := tlsClient.Do(req)
				checkErr(t, err)
				if resp.StatusCode != http.StatusForbidden {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
				}
				expected := "cluster user \"web\" is not allowed to access"
				checkResponse(t, resp.Body, expected)
				resp.Body.Close()
			},
			startTLS,
		},
		{
			"routing",
			"testdata/http.yml",
			func(t *testing.T) {
				req, err := http.NewRequest(http.MethodOptions, "http://127.0.0.1:9090?query=asd", nil)
				checkErr(t, err)
				resp, err := http.DefaultClient.Do(req)
				checkErr(t, err)
				expectedAllowHeader := "GET,POST"
				if resp.Header.Get("Allow") != expectedAllowHeader {
					t.Fatalf("header `Allow` got: %q; expected: %q", resp.Header.Get("Allow"), expectedAllowHeader)
				}
				resp.Body.Close()

				req, err = http.NewRequest(http.MethodConnect, "http://127.0.0.1:9090?query=asd", nil)
				checkErr(t, err)
				resp, err = http.DefaultClient.Do(req)
				checkErr(t, err)
				expected := fmt.Sprintf("unsupported method %q", http.MethodConnect)
				checkResponse(t, resp.Body, expected)
				if resp.StatusCode != http.StatusMethodNotAllowed {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusMethodNotAllowed)
				}
				resp.Body.Close()

				resp = httpGet(t, "http://127.0.0.1:9090/foobar", http.StatusBadRequest)
				expected = fmt.Sprintf("unsupported path: \"/foobar\"")
				checkResponse(t, resp.Body, expected)
				resp.Body.Close()
			},
			startHTTP,
		},
		{
			"http POST request with session id",
			"testdata/http-session-id.yml",
			func(t *testing.T) {
				req, err := http.NewRequest("POST",
					"http://127.0.0.1:9090/?query_id=45395792-a432-4b92-8cc9-536c14e1e1a9&extremes=0&session_id=default-session-id233",
					bytes.NewBufferString("SELECT * FROM system.numbers LIMIT 10"))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded;") // This makes it work

				checkErr(t, err)
				resp, err := http.DefaultClient.Do(req)
				checkErr(t, err)

				if resp.StatusCode != http.StatusOK || resp.StatusCode != http.StatusOK && resp.Header.Get("X-Clickhouse-Server-Session-Id") == "" {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
				}
				resp.Body.Close()
			},
			startHTTP,
		},
		{
			"http request",
			"testdata/http.yml",
			func(t *testing.T) {
				httpGet(t, "http://127.0.0.1:9090?query=asd", http.StatusOK)
				httpGet(t, "http://127.0.0.1:9090/metrics", http.StatusOK)
			},
			startHTTP,
		},
		{
			"http requests with caching in redis ",
			"testdata/http.cache.redis.yml",
			func(t *testing.T) {

				q := "SELECT redis_cache_mate"
				req, err := http.NewRequest("GET", "http://127.0.0.1:9090?query="+url.QueryEscape(q), nil)
				checkErr(t, err)

				resp := httpRequest(t, req, http.StatusOK)
				checkResponse(t, resp.Body, expectedOkResp)
				resp2 := httpRequest(t, req, http.StatusOK)
				checkResponse(t, resp2.Body, expectedOkResp)
				keys := redisClient.Keys()
				if len(keys) != 2 { // 2 because there is a record stored for transaction and a cache item
					t.Fatalf("unexpected amount of keys in redis: %v", len(keys))
				}

				// check cached response
				key := &cache.Key{
					Query:          []byte(q),
					AcceptEncoding: "gzip",
					Version:        cache.Version,
				}
				str, err := redisClient.Get(key.String())
				checkErr(t, err)

				if !strings.Contains(str, base64.StdEncoding.EncodeToString([]byte("Ok."))) || !strings.Contains(str, "text/plain") || !strings.Contains(str, "charset=utf-8") {
					t.Fatalf("result from cache query is wrong: %s", str)
				}

				duration := redisClient.TTL(key.String())
				if duration > 1*time.Minute || duration < 30*time.Second {
					t.Fatalf("ttl on redis key was wrongly set: %s", duration.String())
				}
			},
			startHTTP,
		},
		{
			"http requests with caching in redis (testcase for base64 encoding/decoding)",
			"testdata/http.cache.redis.yml",
			func(t *testing.T) {
				redisClient.FlushAll()
				q := "SELECT 1 FORMAT TabSeparatedWithNamesAndTypes"
				req, err := http.NewRequest("GET", "http://127.0.0.1:9090?query="+url.QueryEscape(q), nil)
				checkErr(t, err)

				resp := httpRequest(t, req, http.StatusOK)
				checkResponse(t, resp.Body, string(bytesWithInvalidUTFPairs))
				resp2 := httpRequest(t, req, http.StatusOK)
				// if we do not use base64 to encode/decode the cached payload, EOF error will be thrown here.
				checkResponse(t, resp2.Body, string(bytesWithInvalidUTFPairs))
				keys := redisClient.Keys()
				if len(keys) != 2 { // 2 because there is a record stored for transaction, and a cache item
					t.Fatalf("unexpected amount of keys in redis: %v", len(keys))
				}

				// check cached response
				key := &cache.Key{
					Query:          []byte(q),
					AcceptEncoding: "gzip",
					Version:        cache.Version,
				}
				str, err := redisClient.Get(key.String())
				checkErr(t, err)

				type redisCachePayload struct {
					Length   int64  `json:"l"`
					Type     string `json:"t"`
					Encoding string `json:"enc"`
					Payload  string `json:"payload"`
				}

				var unMarshaledPayload redisCachePayload
				err = json.Unmarshal([]byte(str), &unMarshaledPayload)
				checkErr(t, err)
				if unMarshaledPayload.Payload != base64.StdEncoding.EncodeToString(bytesWithInvalidUTFPairs) {
					t.Fatalf("result from cache query is wrong: %s", str)
				}
				decoded, err := base64.StdEncoding.DecodeString(unMarshaledPayload.Payload)
				checkErr(t, err)

				if unMarshaledPayload.Length != int64(len(decoded)) {
					t.Fatalf("the declared length %d and actual length %d is not same", unMarshaledPayload.Length, len(decoded))
				}
			},
			startHTTP,
		},

		{
			"http gzipped POST request",
			"testdata/http.cache.yml",
			func(t *testing.T) {
				var buf bytes.Buffer
				zw := gzip.NewWriter(&buf)
				_, err := zw.Write([]byte("SELECT * FROM system.numbers LIMIT 10"))
				checkErr(t, err)
				zw.Close()
				req, err := http.NewRequest("POST", "http://127.0.0.1:9090", &buf)
				checkErr(t, err)
				req.Header.Set("Content-Encoding", "gzip")
				resp, err := http.DefaultClient.Do(req)
				checkErr(t, err)
				body, _ := ioutil.ReadAll(resp.Body)
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("unexpected status code: %d; expected: %d; body: %s", resp.StatusCode, http.StatusOK, string(body))
				}
				resp.Body.Close()
			},
			startHTTP,
		},
		{
			"http POST request",
			"testdata/http.yml",
			func(t *testing.T) {
				buf := bytes.NewBufferString("SELECT * FROM system.numbers LIMIT 10")
				req, err := http.NewRequest("POST", "http://127.0.0.1:9090", buf)
				checkErr(t, err)
				resp, err := http.DefaultClient.Do(req)
				checkErr(t, err)
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
				}
				resp.Body.Close()
			},
			startHTTP,
		},
		{
			"http gzipped POST execution time",
			"testdata/http.execution.time.yml",
			func(t *testing.T) {
				var buf bytes.Buffer
				zw := gzip.NewWriter(&buf)
				_, err := zw.Write([]byte("SELECT * FROM system.numbers LIMIT 1000"))
				checkErr(t, err)
				zw.Close()
				req, err := http.NewRequest("POST", "http://127.0.0.1:9090", &buf)
				checkErr(t, err)
				req.Header.Set("Content-Encoding", "gzip")
				resp, err := http.DefaultClient.Do(req)
				checkErr(t, err)
				if resp.StatusCode != http.StatusGatewayTimeout {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusGatewayTimeout)
				}
				resp.Body.Close()
			},
			startHTTP,
		},
		{
			"deny http",
			"testdata/http.deny.http.yml",
			func(t *testing.T) {
				resp := httpGet(t, "http://127.0.0.1:9090?query=asd", http.StatusForbidden)
				expected := "user \"default\" is not allowed to access via http"
				checkResponse(t, resp.Body, expected)
				resp.Body.Close()
			},
			startHTTP,
		},
		{
			"http networks",
			"testdata/http.networks.yml",
			func(t *testing.T) {
				resp := httpGet(t, "http://127.0.0.1:9090?query=asd", http.StatusForbidden)
				expected := "http connections are not allowed from 127.0.0.1"
				checkResponse(t, resp.Body, expected)
				resp.Body.Close()
			},
			startHTTP,
		},
		{
			"http metrics networks",
			"testdata/http.metrics.networks.yml",
			func(t *testing.T) {
				resp := httpGet(t, "http://127.0.0.1:9090/metrics", http.StatusForbidden)
				expected := "connections to /metrics are not allowed from 127.0.0.1"
				checkResponse(t, resp.Body, expected)
				resp.Body.Close()
			},
			startHTTP,
		},
		{
			"http user networks",
			"testdata/http.user.networks.yml",
			func(t *testing.T) {
				resp := httpGet(t, "http://127.0.0.1:9090?query=asd", http.StatusForbidden)
				expected := "user \"default\" is not allowed to access"
				checkResponse(t, resp.Body, expected)
				resp.Body.Close()
			},
			startHTTP,
		},
		{
			"http cluster user networks",
			"testdata/http.cluster.user.networks.yml",
			func(t *testing.T) {
				resp := httpGet(t, "http://127.0.0.1:9090?query=asd", http.StatusForbidden)
				expected := "cluster user \"web\" is not allowed to access"
				checkResponse(t, resp.Body, expected)
				resp.Body.Close()
			},
			startHTTP,
		},
		{
			"http shared cache",
			"testdata/http.shared.cache.yml",
			func(t *testing.T) {
				// actually we can check that cache-file is shared via metrics
				// but it needs additional library for doing this
				// so it would be just files amount check
				cacheDir := "temp-test-data/shared_cache"
				checkFilesCount(t, cacheDir, 0)
				httpGet(t, "http://127.0.0.1:9090?query=SELECT&user=user1", http.StatusOK)
				checkFilesCount(t, cacheDir, 1)
				httpGet(t, "http://127.0.0.1:9090?query=SELECT&user=user2", http.StatusOK)
				// request from different user expected to be served with already cached,
				// so count of files should be the same
				checkFilesCount(t, cacheDir, 1)
			},
			startHTTP,
		},
		{
			"http cached gzipped deadline",
			"testdata/http.cache.deadline.yml",
			func(t *testing.T) {
				var buf bytes.Buffer
				zw := gzip.NewWriter(&buf)
				_, err := zw.Write([]byte("SELECT SLEEP"))
				checkErr(t, err)
				zw.Close()
				req, err := http.NewRequest("POST", "http://127.0.0.1:9090", &buf)
				checkErr(t, err)
				req.Header.Set("Content-Encoding", "gzip")

				cacheDir := "temp-test-data/cache_deadline"
				checkFilesCount(t, cacheDir, 0)

				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(100*time.Millisecond))
				defer cancel()
				req = req.WithContext(ctx)
				_, err = http.DefaultClient.Do(req)
				expErr := "context deadline exceeded"
				if err == nil {
					t.Fatal("expected deadline error")
				}

				if !strings.Contains(err.Error(), "context deadline exceeded") {
					t.Fatalf("unexpected error: %s; expected: %s", err, expErr)
				}
				// verifies if query isn't cached and if kill query was launched.
				select {
				case <-fakeCHState.syncCH:
					// wait while chproxy will detect that request was canceled and will drop temp file
					time.Sleep(time.Millisecond * 200)
					checkFilesCount(t, cacheDir, 0)
				case <-time.After(time.Second * 5):
					t.Fatalf("expected deadline query to be killed")
				}
			},
			startHTTP,
		},
		{
			"http concurrent transaction fails after grace time",
			"testdata/http.concurrent.transaction.yml",
			func(t *testing.T) {
				// max_exec_time = 300 ms, grace_time = 160 ms (> max_exec_time/2)
				// scenario: 2 concurrent queries. First one hits clickhouse, second one awaits concurrent query.
				// First query takes more than grace time to compute, hence the second query fails with timeout status.
				q := "SELECT SLEEP200"
				executeTwoConcurrentRequests(t, q, 200, 408, "", "no result found during grace time period")
			},
			startHTTP,
		},
		{
			"http concurrent transaction from cache",
			"testdata/http.concurrent.transaction.yml",
			func(t *testing.T) {

				// max_exec_time = 300 ms, grace_time = 160 ms (> max_exec_time/2)
				// scenario: 1st query completes before grace_time elapsed. 2nd query is served from cache.

				q := "SELECT SLEEP100"
				executeTwoConcurrentRequests(t, q, http.StatusOK, http.StatusOK, "success mate", "success mate")
			},
			startHTTP,
		},
		{
			"http concurrent transaction failure scenario",
			"testdata/http.concurrent.transaction.yml",
			func(t *testing.T) {
				// max_exec_time = 300 ms, grace_time = 160 ms (> max_exec_time/2)
				// scenario: 1st query fails before grace_time elapsed. 2nd query fails as well.

				q := "SELECT ERROR"
				executeTwoConcurrentRequests(t, q, http.StatusTeapot, http.StatusInternalServerError, "", "concurrent query failed")
			},
			startHTTP,
		},
	}

	// Wait until CHServer starts.
	go startCHServer()
	redisClient = startRedis(t)
	startTime := time.Now()
	i := 0
	for i < 10 {
		if _, err := http.Get("http://127.0.0.1:8124/"); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
		i++
	}
	if i >= 10 {
		t.Fatalf("CHServer didn't start in %s", time.Since(startTime))
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			*configFile = tc.file
			ln, done := tc.listenFn()
			defer func() {
				if err := ln.Close(); err != nil {
					t.Fatalf("unexpected error while closing listener: %s", err)
				}
				<-done
			}()

			var c *cluster
			for _, cluster := range proxy.clusters {
				c = cluster
				break
			}
			var i int
			for {
				if i > 3 {
					t.Fatal("unable to find active hosts")
				}
				if h := c.getHost(); h != nil {
					break
				}
				i++
				time.Sleep(time.Millisecond * 10)
			}

			tc.testFn(t)
		})
	}
}

func checkFilesCount(t *testing.T, dir string, expectedLen int) {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		t.Fatalf("error while reading dir %q: %s", dir, err)
	}
	if expectedLen != len(files) {
		t.Fatalf("expected %d files in cache dir; got: %d", expectedLen, len(files))
	}
}

var tlsClient = &http.Client{Transport: &http.Transport{
	TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
}}

func startTLS() (net.Listener, chan struct{}) {
	cfg, err := loadConfig()
	if err != nil {
		panic(fmt.Sprintf("error while loading config: %s", err))
	}
	if err = applyConfig(cfg); err != nil {
		panic(fmt.Sprintf("error while applying config: %s", err))
	}
	done := make(chan struct{})
	ln, err := net.Listen("tcp4", cfg.Server.HTTPS.ListenAddr)
	if err != nil {
		panic(fmt.Sprintf("cannot listen for %q: %s", cfg.Server.HTTPS.ListenAddr, err))
	}
	tlsCfg := newTLSConfig(cfg.Server.HTTPS)
	tln := tls.NewListener(ln, tlsCfg)
	h := http.HandlerFunc(serveHTTP)
	go func() {
		listenAndServe(tln, h, config.TimeoutCfg{})
		close(done)
	}()
	return tln, done
}

func startHTTP() (net.Listener, chan struct{}) {
	cfg, err := loadConfig()
	if err != nil {
		panic(fmt.Sprintf("error while loading config: %s", err))
	}
	if err = applyConfig(cfg); err != nil {
		panic(fmt.Sprintf("error while applying config: %s", err))
	}
	done := make(chan struct{})
	ln, err := net.Listen("tcp4", cfg.Server.HTTP.ListenAddr)
	if err != nil {
		panic(fmt.Sprintf("cannot listen for %q: %s", cfg.Server.HTTP.ListenAddr, err))
	}
	h := http.HandlerFunc(serveHTTP)
	go func() {
		listenAndServe(ln, h, config.TimeoutCfg{})
		close(done)
	}()
	return ln, done
}

// TODO randomise port for each instance of the mock
func startRedis(t *testing.T) *miniredis.Miniredis {
	redis := miniredis.NewMiniRedis()
	if err := redis.StartAddr("127.0.0.1:6379"); err != nil {
		t.Fatalf("Failed to create redis server: %s", err)
	}
	return redis
}

// TODO randomise port for each instance of the mock
func startCHServer() {
	http.ListenAndServe(":8124", http.HandlerFunc(fakeCHHandler))
}

var patt = regexp.MustCompile(`(\d+)$`)

func fakeCHHandler(w http.ResponseWriter, r *http.Request) {
	query, err := getFullQuery(r)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "error while reading query: %s", err)
		return
	}
	if len(query) == 0 && r.Method != http.MethodGet {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "got empty query for non-GET request")
		return
	}
	defer func() {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}()
	q := string(query)
	switch {
	case q == "SELECT ERROR":
		w.WriteHeader(http.StatusTeapot)
		fmt.Fprint(w, "DB::Exception\n")
	case q == "SELECT SLEEP":
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "foo")
		fakeCHState.sleep()
		fmt.Fprint(w, "bar")

	case strings.Contains(q, "SELECT SLEEP"):
		numberStrings := patt.FindAllStringSubmatch(q, -1)
		if len(numberStrings) == 0 {
			panic("couldn't extract number from sleep call to clickhouse mock")
		}
		nr, err := strconv.Atoi(numberStrings[0][1])
		if err != nil {
			panic(err)
		}

		duration, err := time.ParseDuration(fmt.Sprintf("%vms", nr))
		if err != nil {
			panic(err)
		}
		time.Sleep(duration)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "success mate") //nolint
	case q == "SELECT 1 FORMAT TabSeparatedWithNamesAndTypes":
		w.WriteHeader(http.StatusOK)
		w.Write(bytesWithInvalidUTFPairs)
	default:
		if strings.Contains(string(query), killQueryPattern) {
			fakeCHState.kill()
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Ok.\n")
	}
}

// executeTwoConcurrentRequests concurrently executes 2 requests for the same query.
// Results are asserted according to the specified input parameters.
func executeTwoConcurrentRequests(t *testing.T, query string, firstStatusCode, secondStatusCode int, firstBody, secondBody string) {
	u := fmt.Sprintf("http://127.0.0.1:9090?query=%s&user=concurrent_user", url.QueryEscape(query))
	req, err := http.NewRequest("GET", u, nil)
	checkErr(t, err)

	var wg sync.WaitGroup
	wg.Add(2)
	var resp1 string
	var resp2 string
	go func() {
		resp := httpRequest(t, req, firstStatusCode)
		resp1 = bbToString(t, resp.Body)
		wg.Done()
	}()

	go func() {
		time.Sleep(20 * time.Millisecond)
		resp := httpRequest(t, req, secondStatusCode)
		resp2 = bbToString(t, resp.Body)
		wg.Done()
	}()
	wg.Wait()

	if !strings.Contains(resp1, firstBody) {
		t.Fatalf("concurrent test scenario: unexpected resp body: %s, expected : %s", resp1, firstBody)
	}

	if !strings.Contains(resp2, secondBody) {
		t.Fatalf("concurrent test scenario: unexpected resp body: %s, expected : %s", resp2, secondBody)
	}
}

var bytesWithInvalidUTFPairs = []byte{239, 191, 189, 1, 32, 50, 239, 191}

var fakeCHState = &stateCH{
	syncCH: make(chan struct{}),
}

type stateCH struct {
	sync.Mutex
	inited bool
	syncCH chan struct{}
}

func (s *stateCH) kill() {
	s.Lock()
	defer s.Unlock()
	if !s.inited {
		return
	}
	close(s.syncCH)
}

func (s *stateCH) sleep() {
	s.Lock()
	s.inited = true
	s.Unlock()
	<-s.syncCH
}

func TestNewTLSConfig(t *testing.T) {
	cfg := config.HTTPS{
		KeyFile:  "testdata/example.com.key",
		CertFile: "testdata/example.com.cert",
	}

	tlsCfg := newTLSConfig(cfg)
	if len(tlsCfg.Certificates) < 1 {
		t.Fatalf("expected tls certificate; got empty list")
	}

	certCachePath := fmt.Sprintf("%s/certs_dir", testDir)
	cfg = config.HTTPS{
		Autocert: config.Autocert{
			CacheDir:     certCachePath,
			AllowedHosts: []string{"example.com"},
		},
	}
	autocertManager = newAutocertManager(cfg.Autocert)
	tlsCfg = newTLSConfig(cfg)
	if tlsCfg.GetCertificate == nil {
		t.Fatalf("expected func GetCertificate be set; got nil")
	}

	if _, err := os.Stat(certCachePath); err != nil {
		t.Fatalf("expected dir %s to be created", certCachePath)
	}
}

func TestReloadConfig(t *testing.T) {
	*configFile = "testdata/http.yml"
	if err := reloadConfig(); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	*configFile = "testdata/foobar.yml"
	if err := reloadConfig(); err == nil {
		t.Fatal("error expected; got nil")
	}
}

func checkErr(t *testing.T, err error) {
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

func checkResponse(t *testing.T, r io.Reader, expected string) {
	if r == nil {
		t.Fatal("unexpected nil reader")
	}
	response, err := ioutil.ReadAll(r)
	if err != nil {
		t.Fatalf("unexpected err while reading: %s", err)
	}
	got := string(response)
	if !strings.Contains(got, expected) {
		t.Fatalf("got: %q; expected: %q", got, expected)
	}
}

func httpGet(t *testing.T, url string, statusCode int) *http.Response {
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("unexpected erorr while doing GET request: %s", err)
	}
	if resp.StatusCode != statusCode {
		t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, statusCode)
	}
	return resp
}

func httpRequest(t *testing.T, request *http.Request, statusCode int) *http.Response {
	t.Helper()
	client := http.Client{}
	resp, err := client.Do(request)
	if err != nil {
		t.Fatalf("unexpected erorr while doing GET request: %s", err)
	}
	if resp.StatusCode != statusCode {
		t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, statusCode)
	}
	return resp
}
