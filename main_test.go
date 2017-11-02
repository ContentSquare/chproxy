package main

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Vertamedia/chproxy/cache"
	"github.com/Vertamedia/chproxy/log"
)

var testDir = "./temp-test-data"

func TestMain(m *testing.M) {
	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		os.Mkdir(testDir, os.ModePerm)
	}
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
	caches, err = cache.New(cfg.Caches)
	if err != nil {
		panic(fmt.Sprintf("cannot initialize caches: %s", err))
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
	caches, err = cache.New(cfg.Caches)
	if err != nil {
		panic(fmt.Sprintf("cannot initialize caches: %s", err))
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
	http.HandleFunc("/", fakeHandler)
	http.ListenAndServe(":8124", nil)
}

func fakeHandler(w http.ResponseWriter, r *http.Request) {
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
				q := "asd"
				req, err := http.NewRequest("GET", "https://127.0.0.1:8443?query="+q, nil)
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
					Query:  []byte(q),
					IsGzip: true,
				}
				path := fmt.Sprintf("%s/cache/%s", testDir, key.String())
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("err while getting file %q info: %s", path, err)
				}
				rw := httptest.NewRecorder()
				cc := caches["https_cache"]
				if err := cc.WriteTo(rw, key); err != nil {
					t.Fatalf("unexpected error while writing reposnse from cache: %s", err)
				}

				expected := "Ok.\n"
				got, err := ioutil.ReadAll(rw.Body)
				if err != nil {
					t.Fatalf("unexpected error while reading body: %s", err)
				}
				if string(got) != expected {
					t.Fatalf("unexpected cache result: %q; expected: %q", string(got), expected)
				}

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
				response, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("unexpected err while reading body: %s", err)
				}
				stringResponse := string(response)
				expErr := "user \"default\" is not allowed to access via https"
				if !strings.Contains(stringResponse, expErr) {
					t.Fatalf("unexpected response: %q; expected: %q", response, expErr)
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
				response, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("unexpected err while reading body: %s", err)
				}
				res := string(response)
				expErr := "https connections are not allowed from 127.0.0.1"
				if !strings.Contains(res, expErr) {
					t.Fatalf("unexpected response: %q; expected: %q", res, expErr)
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
				response, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("unexpected err while reading body: %s", err)
				}
				res := string(response)
				expErr := "user \"default\" is not allowed to access"
				if !strings.Contains(res, expErr) {
					t.Fatalf("unexpected response: %q; expected: %q", res, expErr)
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
				response, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("unexpected err while reading body: %s", err)
				}
				res := string(response)
				expErr := "cluster user \"web\" is not allowed to access"
				if !strings.Contains(res, expErr) {
					t.Fatalf("unexpected response: %q; expected: %q", res, expErr)
				}
				resp.Body.Close()
			},
			startTLS,
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
				response, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("unexpected err while reading body: %s", err)
				}
				stringResponse := string(response)
				expErr := "user \"default\" is not allowed to access via http"
				if !strings.Contains(stringResponse, expErr) {
					t.Fatalf("unexpected response: %q; expected: %q", response, expErr)
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
				response, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("unexpected err while reading body: %s", err)
				}
				res := string(response)
				expErr := "http connections are not allowed from 127.0.0.1"
				if !strings.Contains(res, expErr) {
					t.Fatalf("unexpected response: %q; expected: %q", res, expErr)
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
				response, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("unexpected err while reading body: %s", err)
				}
				res := string(response)
				expErr := "connections to /metrics are not allowed from 127.0.0.1"
				if !strings.Contains(res, expErr) {
					t.Fatalf("unexpected response: %q; expected: %q", res, expErr)
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
				response, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("unexpected err while reading body: %s", err)
				}
				res := string(response)
				expErr := "user \"default\" is not allowed to access"
				if !strings.Contains(res, expErr) {
					t.Fatalf("unexpected response: %q; expected: %q", res, expErr)
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
				response, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("unexpected err while reading body: %s", err)
				}
				res := string(response)
				expErr := "cluster user \"web\" is not allowed to access"
				if !strings.Contains(res, expErr) {
					t.Fatalf("unexpected response: %q; expected: %q", res, expErr)
				}
				resp.Body.Close()
			},
			startHTTP,
		},
	}

	go startCHServer()
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
			tc.testFn(t)
		})
	}
}
