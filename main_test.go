package main

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Vertamedia/chproxy/cache"
	"github.com/Vertamedia/chproxy/config"
	"github.com/Vertamedia/chproxy/log"
)

var testDir = "./temp-test-data"

func TestMain(m *testing.M) {
	log.SuppressOutput(true)
	retCode := m.Run()
	log.SuppressOutput(false)
	if err := os.RemoveAll(testDir); err != nil {
		log.Fatalf("cannot remove %q: %s", testDir, err)
	}
	os.Exit(retCode)
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
	go func() {
		listenAndServe(tln, time.Minute)
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
	go func() {
		listenAndServe(ln, time.Minute)
		close(done)
	}()
	return ln, done
}

func startCHServer() {
	http.ListenAndServe(":8124", http.HandlerFunc(fakeHandler))
}

func fakeHandler(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprint(w, "Ok.\n")
}

func TestServe(t *testing.T) {
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
				if err != nil {
					t.Fatalf("unexpected erorr: %s", err)
				}
				resp, err := tlsClient.Do(req)
				if err != nil {
					t.Fatalf("unexpected error: %s", err)
				}
				if resp.StatusCode != http.StatusUnauthorized {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusUnauthorized)
				}
				resp.Body.Close()

				req, err = http.NewRequest("GET", "https://127.0.0.1:8443?query=asd", nil)
				if err != nil {
					t.Fatalf("unexpected erorr: %s", err)
				}
				req.SetBasicAuth("default", "qwerty")
				resp, err = tlsClient.Do(req)
				if err != nil {
					t.Fatalf("unexpected error: %s", err)
				}
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
				q := "SELECT 123"
				req, err := http.NewRequest("GET", "https://127.0.0.1:8443?query="+url.QueryEscape(q), nil)
				if err != nil {
					t.Fatalf("unexpected erorr: %s", err)
				}
				req.SetBasicAuth("default", "qwerty")
				resp, err := tlsClient.Do(req)
				if err != nil {
					t.Fatalf("unexpected error: %s", err)
				}
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
				}
				resp.Body.Close()
				key := &cache.Key{
					Query:          []byte(q),
					AcceptEncoding: "gzip",
				}
				path := fmt.Sprintf("%s/cache/%s", testDir, key.String())
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("err while getting file %q info: %s", path, err)
				}
				rw := httptest.NewRecorder()
				cc := proxy.caches["https_cache"]
				if err := cc.WriteTo(rw, key); err != nil {
					t.Fatalf("unexpected error while writing reposnse from cache: %s", err)
				}

				expected := "Ok.\n"
				got := bbToString(t, rw.Body)
				if got != expected {
					t.Fatalf("unexpected cache result: %q; expected: %q", string(got), expected)
				}

				// check refreshCacheMetrics()
				req, err = http.NewRequest("GET", "https://127.0.0.1:8443/metrics", nil)
				if err != nil {
					t.Fatalf("unexpected erorr: %s", err)
				}
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
				if err != nil {
					t.Fatalf("unexpected erorr: %s", err)
				}
				req.SetBasicAuth("default", "qwerty")
				resp, err := tlsClient.Do(req)
				if err != nil {
					t.Fatalf("unexpected error: %s", err)
				}
				if resp.StatusCode != http.StatusForbidden {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
				}
				got := bbToString(t, resp.Body)
				expErr := "user \"default\" is not allowed to access via https"
				if !strings.Contains(got, expErr) {
					t.Fatalf("unexpected response: %q; expected: %q", got, expErr)
				}
				resp.Body.Close()
			},
			startTLS,
		},
		{
			"https networks",
			"testdata/https.networks.yml",
			func(t *testing.T) {
				req, err := http.NewRequest("GET", "https://127.0.0.1:8443?query=asd", nil)
				if err != nil {
					t.Fatalf("unexpected erorr: %s", err)
				}
				req.SetBasicAuth("default", "qwerty")
				resp, err := tlsClient.Do(req)
				if err != nil {
					t.Fatalf("unexpected error: %s", err)
				}
				if resp.StatusCode != http.StatusForbidden {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
				}
				got := bbToString(t, resp.Body)
				expErr := "https connections are not allowed from 127.0.0.1"
				if !strings.Contains(got, expErr) {
					t.Fatalf("unexpected response: %q; expected: %q", got, expErr)
				}
				resp.Body.Close()
			},
			startTLS,
		},
		{
			"https user networks",
			"testdata/https.user.networks.yml",
			func(t *testing.T) {
				req, err := http.NewRequest("GET", "https://127.0.0.1:8443?query=asd", nil)
				if err != nil {
					t.Fatalf("unexpected erorr: %s", err)
				}
				req.SetBasicAuth("default", "qwerty")
				resp, err := tlsClient.Do(req)
				if err != nil {
					t.Fatalf("unexpected erorr: %s", err)
				}
				if resp.StatusCode != http.StatusForbidden {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
				}
				got := bbToString(t, resp.Body)
				expErr := "user \"default\" is not allowed to access"
				if !strings.Contains(got, expErr) {
					t.Fatalf("unexpected response: %q; expected: %q", got, expErr)
				}
				resp.Body.Close()
			},
			startTLS,
		},
		{
			"https cluster user networks",
			"testdata/https.cluster.user.networks.yml",
			func(t *testing.T) {
				req, err := http.NewRequest("GET", "https://127.0.0.1:8443?query=asd", nil)
				if err != nil {
					t.Fatalf("unexpected erorr: %s", err)
				}
				req.SetBasicAuth("default", "qwerty")
				resp, err := tlsClient.Do(req)
				if err != nil {
					t.Fatalf("unexpected erorr: %s", err)
				}
				if resp.StatusCode != http.StatusForbidden {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
				}
				got := bbToString(t, resp.Body)
				expErr := "cluster user \"web\" is not allowed to access"
				if !strings.Contains(got, expErr) {
					t.Fatalf("unexpected response: %q; expected: %q", got, expErr)
				}
				resp.Body.Close()
			},
			startTLS,
		},
		{
			"routing",
			"testdata/http.yml",
			func(t *testing.T) {
				req, err := http.NewRequest(http.MethodOptions, "http://127.0.0.1:9090?query=asd", nil)
				if err != nil {
					t.Fatalf("unexpected erorr: %s", err)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				expectedAllowHeader := "GET,POST"
				if resp.Header.Get("Allow") != expectedAllowHeader {
					t.Fatalf("header `Allow` got: %q; expected: %q", resp.Header.Get("Allow"), expectedAllowHeader)
				}
				resp.Body.Close()

				req, err = http.NewRequest(http.MethodConnect, "http://127.0.0.1:9090?query=asd", nil)
				if err != nil {
					t.Fatalf("unexpected erorr: %s", err)
				}
				resp, err = http.DefaultClient.Do(req)
				if err != nil {
					t.Fatalf("unexpected erorr: %s", err)
				}
				got := bbToString(t, resp.Body)
				expErr := fmt.Sprintf("unsupported method %q\n", http.MethodConnect)
				if got != expErr {
					t.Fatalf("unexpected response: %s; expected: %s", got, expErr)
				}
				if resp.StatusCode != http.StatusMethodNotAllowed {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusMethodNotAllowed)
				}
				resp.Body.Close()

				resp, err = http.Get("http://127.0.0.1:9090/foobar")
				if err != nil {
					t.Fatalf("unexpected erorr: %s", err)
				}
				got = bbToString(t, resp.Body)
				expErr = fmt.Sprintf("unsupported path: /foobar")
				if got != expErr {
					t.Fatalf("unexpected response: %s; expected: %s", got, expErr)
				}
				if resp.StatusCode != http.StatusBadRequest {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusBadRequest)
				}
				resp.Body.Close()
			},
			startHTTP,
		},
		{
			"http request",
			"testdata/http.yml",
			func(t *testing.T) {
				resp, err := http.Get("http://127.0.0.1:9090?query=asd")
				if err != nil {
					t.Fatalf("unexpected erorr: %s", err)
				}
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
				}
				resp, err = http.Get("http://127.0.0.1:9090/metrics")
				if err != nil {
					t.Fatalf("unexpected error while doing request: %s", err)
				}
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
				}
				resp.Body.Close()
			},
			startHTTP,
		},
		{
			"http gzipped POST request",
			"testdata/http.yml",
			func(t *testing.T) {
				var buf bytes.Buffer
				zw := gzip.NewWriter(&buf)
				_, err := zw.Write([]byte("SELECT * FROM system.numbers LIMIT 10"))
				if err != nil {
					t.Fatal(err)
				}
				zw.Close()
				req, err := http.NewRequest("POST", "http://127.0.0.1:9090", &buf)
				req.Header.Set("Content-Encoding", "gzip")
				if err != nil {
					t.Fatal(err)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
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
				if err != nil {
					t.Fatal(err)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
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
				if err != nil {
					t.Fatal(err)
				}
				zw.Close()
				req, err := http.NewRequest("POST", "http://127.0.0.1:9090", &buf)
				req.Header.Set("Content-Encoding", "gzip")
				if err != nil {
					t.Fatal(err)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != http.StatusBadGateway {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusBadGateway)
				}
				resp.Body.Close()
			},
			startHTTP,
		},
		{
			"deny http",
			"testdata/http.deny.http.yml",
			func(t *testing.T) {
				resp, err := http.Get("http://127.0.0.1:9090?query=asd")
				if err != nil {
					t.Fatalf("unexpected erorr: %s", err)
				}
				if resp.StatusCode != http.StatusForbidden {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
				}
				got := bbToString(t, resp.Body)
				expErr := "user \"default\" is not allowed to access via http"
				if !strings.Contains(got, expErr) {
					t.Fatalf("unexpected response: %q; expected: %q", got, expErr)
				}
				resp.Body.Close()
			},
			startHTTP,
		},
		{
			"http networks",
			"testdata/http.networks.yml",
			func(t *testing.T) {
				resp, err := http.Get("http://127.0.0.1:9090?query=asd")
				if err != nil {
					t.Fatalf("unexpected erorr: %s", err)
				}
				if resp.StatusCode != http.StatusForbidden {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
				}
				got := bbToString(t, resp.Body)
				expErr := "http connections are not allowed from 127.0.0.1"
				if !strings.Contains(got, expErr) {
					t.Fatalf("unexpected response: %q; expected: %q", got, expErr)
				}
				resp.Body.Close()
			},
			startHTTP,
		},
		{
			"http metrics networks",
			"testdata/http.metrics.networks.yml",
			func(t *testing.T) {
				resp, err := http.Get("http://127.0.0.1:9090/metrics")
				if err != nil {
					t.Fatalf("unexpected erorr: %s", err)
				}
				if resp.StatusCode != http.StatusForbidden {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
				}
				got := bbToString(t, resp.Body)
				expErr := "connections to /metrics are not allowed from 127.0.0.1"
				if !strings.Contains(got, expErr) {
					t.Fatalf("unexpected response: %q; expected: %q", got, expErr)
				}
				resp.Body.Close()
			},
			startHTTP,
		},
		{
			"http user networks",
			"testdata/http.user.networks.yml",
			func(t *testing.T) {
				resp, err := http.Get("http://127.0.0.1:9090?query=asd")
				if err != nil {
					t.Fatalf("unexpected erorr: %s", err)
				}
				if resp.StatusCode != http.StatusForbidden {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
				}
				got := bbToString(t, resp.Body)
				expErr := "user \"default\" is not allowed to access"
				if !strings.Contains(got, expErr) {
					t.Fatalf("unexpected response: %q; expected: %q", got, expErr)
				}
				resp.Body.Close()
			},
			startHTTP,
		},
		{
			"http cluster user networks",
			"testdata/http.cluster.user.networks.yml",
			func(t *testing.T) {
				resp, err := http.Get("http://127.0.0.1:9090?query=asd")
				if err != nil {
					t.Fatalf("unexpected erorr: %s", err)
				}
				if resp.StatusCode != http.StatusForbidden {
					t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
				}
				got := bbToString(t, resp.Body)
				expErr := "cluster user \"web\" is not allowed to access"
				if !strings.Contains(got, expErr) {
					t.Fatalf("unexpected response: %q; expected: %q", got, expErr)
				}
				resp.Body.Close()
			},
			startHTTP,
		},
	}

	// Wait until CHServer starts.
	go startCHServer()
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
	tlsCfg = newTLSConfig(cfg)
	if tlsCfg.GetCertificate == nil {
		t.Fatalf("expected func GetCertificate be set; got nil")
	}

	if _, err := os.Stat(certCachePath); err != nil {
		t.Fatalf("expected dir %s to be created", certCachePath)
	}
}

func TestGetMaxResponseTime(t *testing.T) {
	cfg := &config.Config{
		Clusters: []config.Cluster{
			{
				ClusterUsers: []config.ClusterUser{
					{
						MaxExecutionTime: 20 * time.Second,
					},
				},
			},
			{
				ClusterUsers: []config.ClusterUser{
					{
						MaxExecutionTime: 30 * time.Second,
					},
					{
						MaxExecutionTime: 10 * time.Second,
					},
				},
			},
		},
		Users: []config.User{
			{
				MaxExecutionTime: 10 * time.Second,
			},
			{
				MaxExecutionTime: 15 * time.Second,
			},
		},
	}

	expected := 30 * time.Second
	if maxTime := getMaxResponseTime(cfg); maxTime != expected {
		t.Fatalf("got %v; expected %v", maxTime, expected)
	}

	expected = time.Minute
	cfg.Users[0].MaxExecutionTime = expected
	if maxTime := getMaxResponseTime(cfg); maxTime != expected {
		t.Fatalf("got %v; expected %v", maxTime, expected)
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

func bbToString(t *testing.T, r io.Reader) string {
	response, err := ioutil.ReadAll(r)
	if err != nil {
		t.Fatalf("unexpected err while reading: %s", err)
	}
	return string(response)
}
