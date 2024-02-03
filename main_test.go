package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"fmt"
	"io"
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

	"github.com/alicebob/miniredis/v2"
	"github.com/contentsquare/chproxy/cache"
	"github.com/contentsquare/chproxy/config"
	"github.com/contentsquare/chproxy/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"

	"github.com/contentsquare/chproxy/global/types"
)

var (
	testDir   = "./temp-test-data"
	labels    = prometheus.Labels{}
	redisPort = types.RedisPort
	chPort    = types.ClickHousePort
)

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
		listenFn func() (*http.Server, chan struct{})
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
				checkHeader(t, resp, "X-Cache", "MISS")

				// check cached response
				credHash, _ := calcCredentialHash("default", "qwerty")
				key := &cache.Key{
					Query:              []byte(q),
					AcceptEncoding:     "gzip",
					Version:            cache.Version,
					UserCredentialHash: credHash,
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
				err = RespondWithData(rw, cachedData.Data, cachedData.ContentMetadata, cachedData.Ttl, XCacheHit, 200, labels)
				if err != nil {
					t.Fatalf("unexpected error while getting response from cache: %s", err)
				}
				checkResponse(t, rw.Body, expectedOkResp)
				checkHeader(t, rw.Result(), "X-Cache", XCacheHit)
			},
			startTLS,
		},
		{
			"https cache max payload size",
			"testdata/https.cache.max-payload-size.yml",
			func(t *testing.T) {
				q := "SELECT MaxPayloadSize"
				req, err := http.NewRequest("GET", "https://127.0.0.1:8443?query="+url.QueryEscape(q), nil)
				checkErr(t, err)
				req.SetBasicAuth("default", "qwerty")
				req.Close = true

				resp, err := tlsClient.Do(req)
				checkErr(t, err)
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
				}

				checkResponse(t, resp.Body, expectedOkResp)
				checkHeader(t, resp, "X-Cache", XCacheNA)

				key := &cache.Key{
					Query:          []byte(q),
					AcceptEncoding: "gzip",
					Version:        cache.Version,
				}

				cc := proxy.caches["https_cache_max_payload_size"]
				cachedData, err := cc.Get(key)

				if cachedData != nil || err == nil {
					t.Fatal("response bigger than maxPayloadSize should not be cached")
				}

				resp.Body.Close()
			},
			startTLS,
		},
		{
			"https cache max payload size not reached",
			"testdata/https.cache.max-payload-size-not-reached.yml",
			func(t *testing.T) {
				q := "SELECT MaxPayloadSize"
				req, err := http.NewRequest("GET", "https://127.0.0.1:8443?query="+url.QueryEscape(q), nil)
				checkErr(t, err)
				req.SetBasicAuth("default", "qwerty")
				req.Close = true

				resp, err := tlsClient.Do(req)
				checkErr(t, err)
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
				}

				checkResponse(t, resp.Body, expectedOkResp)

				credHash, _ := calcCredentialHash("default", "qwerty")
				key := &cache.Key{
					Query:              []byte(q),
					AcceptEncoding:     "gzip",
					Version:            cache.Version,
					UserCredentialHash: credHash,
				}

				rw := httptest.NewRecorder()

				cc := proxy.caches["https_cache_max_payload_size"]
				cachedData, err := cc.Get(key)

				if err != nil {
					t.Fatalf("unexpected error while getting response from cache: %s", err)
				}

				err = RespondWithData(rw, cachedData.Data, cachedData.ContentMetadata, cachedData.Ttl, XCacheHit, 200, labels)
				if err != nil {
					t.Fatalf("unexpected error while getting response from cache: %s", err)
				}
				checkResponse(t, rw.Body, expectedOkResp)

				cachedData.Data.Close()
				resp.Body.Close()
			},
			startTLS,
		},
		{
			"https request body is not empty",
			"testdata/https.yml",
			func(t *testing.T) {
				query := "SELECT SleepTimeout"
				buf := bytes.NewBufferString(query)
				req, err := http.NewRequest("POST", "https://127.0.0.1:8443", buf)
				checkErr(t, err)
				req.SetBasicAuth("default", "qwerty")
				req.Close = true

				resp, err := tlsClient.Do(req)
				checkErr(t, err)
				if resp.StatusCode != http.StatusGatewayTimeout {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusGatewayTimeout)
				}

				bodyBytes, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("error while reading body from response; err: %q", err)
				}

				b := string(bodyBytes)
				if !strings.Contains(b, query) {
					t.Fatalf("expected request body: %q; got: %q", query, b)
				}

				resp.Body.Close()
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
				credHash, _ := calcCredentialHash("default", "qwerty")
				key := &cache.Key{
					Query:              []byte(expectedQuery),
					AcceptEncoding:     "gzip",
					Version:            cache.Version,
					UserCredentialHash: credHash,
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

				err = RespondWithData(rw, cachedData.Data, cachedData.ContentMetadata, cachedData.Ttl, XCacheMiss, 200, labels)
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
				if resp.StatusCode != http.StatusInternalServerError {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode,
						http.StatusInternalServerError)
				}
				resp.Body.Close()

				// check cached response
				credHash, _ := calcCredentialHash("default", "qwerty")
				key := &cache.Key{
					Query:              []byte(q),
					AcceptEncoding:     "gzip",
					Version:            cache.Version,
					UserCredentialHash: credHash,
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
				keys := redisClient.Keys()
				// redis should be empty before the test
				if len(keys) != 0 {
					t.Fatalf("unexpected amount of keys in redis: %v", len(keys))
				}

				resp, err := httpRequest(t, req, http.StatusOK)
				checkErr(t, err)
				checkResponse(t, resp.Body, expectedOkResp)
				resp2, err := httpRequest(t, req, http.StatusOK)
				checkErr(t, err)
				checkResponse(t, resp2.Body, expectedOkResp)
				keys = redisClient.Keys()
				if len(keys) != 2 { // expected 2 because there is a record stored for transaction and a cache item
					t.Fatalf("unexpected amount of keys in redis: %v", len(keys))
				}

				// check cached response
				credHash, _ := calcCredentialHash("default", "")
				key := &cache.Key{
					Query:              []byte(q),
					AcceptEncoding:     "gzip",
					Version:            cache.Version,
					UserCredentialHash: credHash,
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
				keys := redisClient.Keys()
				// redis should be empty before the test
				if len(keys) != 0 {
					t.Fatalf("unexpected amount of keys in redis: %v", len(keys))
				}

				req, err := http.NewRequest("GET", "http://127.0.0.1:9090?query="+url.QueryEscape(q), nil)
				checkErr(t, err)

				resp, err := httpRequest(t, req, http.StatusOK)
				checkErr(t, err)
				checkResponse(t, resp.Body, string(bytesWithInvalidUTFPairs))
				resp2, err := httpRequest(t, req, http.StatusOK)
				checkErr(t, err)
				// if we do not use base64 to encode/decode the cached payload, EOF error will be thrown here.
				checkResponse(t, resp2.Body, string(bytesWithInvalidUTFPairs))
				keys = redisClient.Keys()
				if len(keys) != 2 { // 2 because there is a record stored for transaction, and a cache item
					t.Fatalf("unexpected amount of keys in redis: %v", len(keys))
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
				body, _ := io.ReadAll(resp.Body)
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("unexpected status code: %d; expected: %d; body: %s", resp.StatusCode, http.StatusOK,
						string(body))
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
			"http metrics namespace",
			"testdata/http.metrics.namespace.yml",
			func(t *testing.T) {
				resp := httpGet(t, "http://127.0.0.1:9090/metrics", http.StatusOK)
				assert.GreaterOrEqual(t, len(getStringFromResponse(t, resp.Body)), 10000)
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
			"http shared cache for them user",
			"testdata/http.shared.cache.yml",
			func(t *testing.T) {
				// actually we can check that cache-file is shared via metrics
				// but it needs additional library for doing this
				// so it would be just files amount check
				cacheDir := "temp-test-data/shared_cache1"
				checkFilesCount(t, cacheDir, 0)
				httpGet(t, "http://127.0.0.1:9090?query=SELECT&user=user1_test1", http.StatusOK)
				checkFilesCount(t, cacheDir, 1)
				httpGet(t, "http://127.0.0.1:9090?query=SELECT&user=user1_test1", http.StatusOK)
				// request from different user expected to be served with already cached,
				// so count of files should be the same
				checkFilesCount(t, cacheDir, 1)
			},
			startHTTP,
		},
		{
			"http not share cache for same user with different pwd",
			"testdata/http.shared.cache.yml",
			func(t *testing.T) {
				// because of the wildcarded feature that delegate the authentication to clickouse
				// we can't afford to return a cached of the same user without reaching clickhouse
				// if the pwd is not the same than the one used for the cached query
				cacheDir := "temp-test-data/shared_cache2"
				checkFilesCount(t, cacheDir, 0)
				httpGet(t, "http://127.0.0.1:9090?query=SELECT&user=user1_test2&password=toto", http.StatusOK)
				checkFilesCount(t, cacheDir, 1)
				httpGet(t, "http://127.0.0.1:9090?query=SELECT&user=user2_test2&password=titi", http.StatusOK)
				checkFilesCount(t, cacheDir, 2)
			},
			startHTTP,
		},
		{
			"http not share cache for different user",
			"testdata/http.shared.cache.yml",
			func(t *testing.T) {
				cacheDir := "temp-test-data/shared_cache3"
				checkFilesCount(t, cacheDir, 0)
				httpGet(t, "http://127.0.0.1:9090?query=SELECT&user=user1_test3", http.StatusOK)
				checkFilesCount(t, cacheDir, 1)
				httpGet(t, "http://127.0.0.1:9090?query=SELECT&user=user2_test3", http.StatusOK)
				checkFilesCount(t, cacheDir, 2)
			},
			startHTTP,
		},
		{
			"http share cache for same user with different pwd",
			"testdata/http.shared.cache.yml",
			func(t *testing.T) {
				// because of the wildcarded feature that delegate the authentication to clickouse
				// we can't afford to return a cached of the same user without reaching clickhouse
				// if the pwd is not the same than the one used for the cached query
				cacheDir := "temp-test-data/shared_cache4"
				checkFilesCount(t, cacheDir, 0)
				httpGet(t, "http://127.0.0.1:9090?query=SELECT&user=user1_test4&password=toto", http.StatusOK)
				checkFilesCount(t, cacheDir, 1)
				httpGet(t, "http://127.0.0.1:9090?query=SELECT&user=user2_test4&password=titi", http.StatusOK)
				checkFilesCount(t, cacheDir, 1)
			},
			startHTTP,
		},
		{
			"http share cache for different user",
			"testdata/http.shared.cache.yml",
			func(t *testing.T) {
				cacheDir := "temp-test-data/shared_cache5"
				checkFilesCount(t, cacheDir, 0)
				httpGet(t, "http://127.0.0.1:9090?query=SELECT&user=user1_test5", http.StatusOK)
				checkFilesCount(t, cacheDir, 1)
				httpGet(t, "http://127.0.0.1:9090?query=SELECT&user=user2_test5", http.StatusOK)
				checkFilesCount(t, cacheDir, 1)
			},
			startHTTP,
		},
		{
			"http not share cache for different user using wildcarded feature",
			"testdata/http.shared.cache.yml",
			func(t *testing.T) {
				cacheDir := "temp-test-data/shared_cache6"
				checkFilesCount(t, cacheDir, 0)
				httpGet(t, "http://127.0.0.1:9090?query=SELECT&user=user1_test6", http.StatusOK)
				checkFilesCount(t, cacheDir, 1)
				httpGet(t, "http://127.0.0.1:9090?query=SELECT&user=user2_test6", http.StatusOK)
				checkFilesCount(t, cacheDir, 2)
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
				executeTwoConcurrentRequests(t, q, http.StatusInternalServerError, http.StatusInternalServerError,
					"DB::Exception\n", "[concurrent query failed] DB::Exception\n")
			},
			startHTTP,
		},
		{
			"http concurrent transaction failure scenario - transaction completed, not failed - query is recoverable",
			"testdata/http.concurrent.transaction.yml",
			func(t *testing.T) {
				// max_exec_time = 300 ms, grace_time = 160 ms (> max_exec_time/2)
				// scenario: 1st query fails before grace_time elapsed. 2nd query fails as well.

				q := "SELECT RECOVERABLE-ERROR"
				executeTwoConcurrentRequests(t, q, http.StatusServiceUnavailable, http.StatusServiceUnavailable,
					"DB::Unavailable\n", "DB::Unavailable\n")
			},
			startHTTP,
		},
		{
			"http request with default proxy headers",
			"testdata/https.proxy-enabled.yml",
			func(t *testing.T) {
				query := "SELECT 1"
				req, err := http.NewRequest("GET", "https://127.0.0.1:8443?query="+url.QueryEscape(query), nil)
				checkErr(t, err)
				req.Close = true
				req.SetBasicAuth("default", "qwerty")
				req.Header.Add("X-Forwarded-For", "10.0.0.1")

				resp, err := tlsClient.Do(req)
				checkErr(t, err)
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
				}

				checkResponse(t, resp.Body, expectedOkResp)
			},
			startTLS,
		},
	}

	// Wait until CHServer starts.
	go startCHServer()
	redisClient = startRedis(t)
	startTime := time.Now()
	i := 0
	for i < 10 {
		if _, err := http.Get(fmt.Sprintf("http://127.0.0.1:%s/", chPort)); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
		i++
	}
	if i >= 10 {
		t.Fatalf("CHServer didn't start in %s", time.Since(startTime))
	}

	cfg := &config.Config{}
	registerMetrics(cfg)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			*configFile = tc.file
			s, done := tc.listenFn()
			defer func() {
				if err := s.Shutdown(context.Background()); err != nil {
					t.Fatalf("unexpected error while closing server: %s", err)
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
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("error while reading dir %q: %s", dir, err)
	}
	if expectedLen != len(files) {
		t.Fatalf("expected %d files in cache dir %s; got: %d", expectedLen, dir, len(files))
	}
}

var tlsClient = &http.Client{Transport: &http.Transport{
	TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
}}

func startTLS() (*http.Server, chan struct{}) {
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
	tlsCfg, err := cfg.Server.HTTPS.TLS.BuildTLSConfig(autocertManager)
	if err != nil {
		panic(fmt.Sprintf("cannot build TLS config: %s", err))
	}
	tln := tls.NewListener(ln, tlsCfg)
	h := http.HandlerFunc(serveHTTP)
	s := newServer(tln, h, config.TimeoutCfg{})
	go func() {
		s.Serve(tln)
		close(done)
	}()
	return s, done
}

func startHTTP() (*http.Server, chan struct{}) {
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
	s := newServer(ln, h, config.TimeoutCfg{})
	go func() {
		s.Serve(ln)
		close(done)
	}()
	return s, done
}

// TODO randomise port for each instance of the mock
func startRedis(t *testing.T) *miniredis.Miniredis {
	redis := miniredis.NewMiniRedis()
	if err := redis.StartAddr("127.0.0.1:" + redisPort); err != nil {
		t.Fatalf("Failed to create redis server: %s", err)
	}
	return redis
}

// TODO randomise port for each instance of the mock
func startCHServer() {
	http.ListenAndServe(":"+chPort, http.HandlerFunc(fakeCHHandler))
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
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "DB::Exception\n")
	case q == "SELECT RECOVERABLE-ERROR":
		println("called clickhouse recoverable")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, "DB::Unavailable\n")
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
	case q == "SELECT MaxPayloadSize":
		w.WriteHeader(http.StatusOK)

		// generate 10M payload size
		b := make([]byte, 10485760)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, b)
		fmt.Fprint(w, "Ok.\n")
	case strings.Contains(q, "SELECT SleepTimeout"):
		w.WriteHeader(http.StatusGatewayTimeout)

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			fmt.Fprintf(w, "query: %s; error while reading body: %s", query, err)
			return
		}

		b := string(bodyBytes)
		// Ensure the original request body is not empty and remains unchanged
		// after it is processed by getFullQuery.
		if b == "" && b != q {
			fmt.Fprintf(w, "got original req body: <%s>; escaped query: <%s>", b, q)
			return
		}

		// execute sleep 1.5 sec
		time.Sleep(1500 * time.Millisecond)
		fmt.Fprint(w, b)
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
func executeTwoConcurrentRequests(t *testing.T, query string, firstStatusCode, secondStatusCode int,
	firstBody, secondBody string) {
	u := fmt.Sprintf("http://127.0.0.1:9090?query=%s&user=concurrent_user", url.QueryEscape(query))

	var wg sync.WaitGroup
	wg.Add(2)
	var resp1 string
	var resp2 string
	errs := make(chan error, 0)
	defer close(errs)
	errors := make([]error, 0)
	go func() {
		for err := range errs {
			errors = append(errors, err)
		}
	}()
	go func() {
		defer wg.Done()
		req, err := http.NewRequest("GET", u, nil)
		checkErr(t, err)
		resp, err := httpRequest(t, req, firstStatusCode)
		if err != nil {
			errs <- err
			return
		}
		resp1 = bbToString(t, resp.Body)
	}()

	go func() {
		defer wg.Done()
		time.Sleep(20 * time.Millisecond)
		req, err := http.NewRequest("GET", u, nil)
		checkErr(t, err)
		resp, err := httpRequest(t, req, secondStatusCode)
		if err != nil {
			errs <- err
			return
		}
		resp2 = bbToString(t, resp.Body)
	}()
	wg.Wait()

	if len(errors) != 0 {
		t.Fatalf("concurrent test scenario failed due to: %v", errors)
	}

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
	cfg := &config.TLS{
		KeyFile:  "testdata/example.com.key",
		CertFile: "testdata/example.com.cert",
	}

	tlsCfg, err := cfg.BuildTLSConfig(autocertManager)
	if err != nil {
		panic(fmt.Sprintf("cannot build TLS config: %s", err))
	}
	if len(tlsCfg.Certificates) < 1 {
		t.Fatalf("expected tls certificate; got empty list")
	}

	certCachePath := fmt.Sprintf("%s/certs_dir", testDir)
	cfg = &config.TLS{
		Autocert: config.Autocert{
			CacheDir:     certCachePath,
			AllowedHosts: []string{"example.com"},
		},
	}
	autocertManager = newAutocertManager(cfg.Autocert)
	if err != nil {
		panic(fmt.Sprintf("cannot build TLS config: %s", err))
	}
	tlsCfg, _ = cfg.BuildTLSConfig(autocertManager)
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

func getStringFromResponse(t *testing.T, r io.Reader) string {
	if r == nil {
		t.Fatalf("unexpected nil reader")
	}
	response, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("unexpected err while reading: %s", err)
	}
	return string(response)
}

func checkResponse(t *testing.T, r io.Reader, expected string) {
	got := getStringFromResponse(t, r)
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

func checkHeader(t *testing.T, resp *http.Response, header string, expected string) {
	t.Helper()

	h := resp.Header
	v := h.Get(header)

	if v != expected {
		t.Fatalf("for header: %s got: %s, expected %s", header, v, expected)
	}
}

func httpRequest(t *testing.T, request *http.Request, statusCode int) (*http.Response, error) {
	t.Helper()
	client := http.Client{}
	resp, err := client.Do(request)
	if err != nil {
		return resp, fmt.Errorf("unexpected erorr while doing GET request: %s", err)
	}
	if resp.StatusCode != statusCode {
		return resp, fmt.Errorf("unexpected status code: %d; expected: %d", resp.StatusCode, statusCode)
	}
	return resp, nil
}
