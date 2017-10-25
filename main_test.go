package main

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
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
	cacheControllers.Store(make(ccList))
	if len(cfg.Caches) > 0 {
		cache.MustRegister(cfg.Caches...)
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
		listenAndServe(tln)
		close(done)
	}()
	return tln, done
}

func startHTTP() (net.Listener, chan struct{}) {
	cfg, err := loadConfig()
	if err != nil {
		panic(fmt.Sprintf("error while loading config: %s", err))
	}
	cacheControllers.Store(make(ccList))
	if len(cfg.Caches) > 0 {
		cache.MustRegister(cfg.Caches...)
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
		listenAndServe(ln)
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
					t.Errorf("unexpected erorr: %s", err)
				}
				resp, err := tlsClient.Do(req)
				if err != nil {
					t.Errorf("unexpected error: %s", err)
				}
				if resp.StatusCode != http.StatusUnauthorized {
					t.Errorf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusUnauthorized)
				}
				resp.Body.Close()

				req, err = http.NewRequest("GET", "https://127.0.0.1:8443?query=asd", nil)
				if err != nil {
					t.Errorf("unexpected erorr: %s", err)
				}
				req.SetBasicAuth("default", "qwerty")
				resp, err = tlsClient.Do(req)
				if err != nil {
					t.Errorf("unexpected error: %s", err)
				}
				if resp.StatusCode != http.StatusOK {
					t.Errorf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
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
					t.Errorf("unexpected erorr: %s", err)
				}
				req.SetBasicAuth("default", "qwerty")
				resp, err := tlsClient.Do(req)
				if err != nil {
					t.Errorf("unexpected error: %s", err)
				}
				if resp.StatusCode != http.StatusOK {
					t.Errorf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
				}
				resp.Body.Close()
				key := cache.GenerateKey([]byte(q))
				path := fmt.Sprintf("%s/cache/%s", testDir, key)
				if _, err := os.Stat(path); err != nil {
					t.Errorf("err while getting file %q info: %s", path, err)
				}
				cc := cache.GetController("https_cache")
				cachedResp, ok := cc.Get(key)
				if !ok {
					t.Errorf("expected key %q to be cached", key)
				}
				expected := "Ok.\n"
				if string(cachedResp) != expected {
					t.Errorf("unexpected cache result: %q; expected: %q", string(cachedResp), expected)
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
					t.Errorf("unexpected erorr: %s", err)
				}
				req.SetBasicAuth("default", "qwerty")
				resp, err := tlsClient.Do(req)
				if err != nil {
					t.Errorf("unexpected error: %s", err)
				}
				if resp.StatusCode != http.StatusForbidden {
					t.Errorf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
				}
				response, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					t.Errorf("unexpected err while reading body: %s", err)
				}
				stringResponse := string(response)
				expErr := "user \"default\" is not allowed to access via https"
				if stringResponse[35:] != expErr {
					t.Errorf("unexpected response: %q; expected: %q", string(response), expErr)
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
					t.Errorf("unexpected erorr: %s", err)
				}
				req.SetBasicAuth("default", "qwerty")
				resp, err := tlsClient.Do(req)
				if err != nil {
					t.Errorf("unexpected error: %s", err)
				}
				if resp.StatusCode != http.StatusForbidden {
					t.Errorf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
				}
				response, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					t.Errorf("unexpected err while reading body: %s", err)
				}
				res := string(response)
				res = res[:48]
				expErr := "https connections are not allowed from 127.0.0.1"
				if res != expErr {
					t.Errorf("unexpected response: %q; expected: %q", res, expErr)
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
					t.Errorf("unexpected erorr: %s", err)
				}
				req.SetBasicAuth("default", "qwerty")
				resp, err := tlsClient.Do(req)
				if err != nil {
					t.Errorf("unexpected erorr: %s", err)
				}
				if resp.StatusCode != http.StatusForbidden {
					t.Errorf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
				}
				response, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					t.Errorf("unexpected err while reading body: %s", err)
				}
				res := string(response)
				res = res[35:]
				expErr := "user \"default\" is not allowed to access"
				if res != expErr {
					t.Errorf("unexpected response: %q; expected: %q", res, expErr)
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
					t.Errorf("unexpected erorr: %s", err)
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
				res = res[35:]
				expErr := "cluster user \"web\" is not allowed to access"
				if res != expErr {
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
					t.Errorf("unexpected erorr: %s", err)
				}
				if resp.StatusCode != http.StatusOK {
					t.Errorf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
				}

				resp, err = http.Get("http://127.0.0.1:9090/metrics")
				if resp.StatusCode != http.StatusOK {
					t.Errorf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
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
					t.Error(err)
				}
				zw.Close()
				req, err := http.NewRequest("POST", "http://127.0.0.1:9090", &buf)
				req.Header.Set("Content-Encoding", "gzip")
				if err != nil {
					t.Error(err)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Error(err)
				}
				if resp.StatusCode != http.StatusOK {
					t.Errorf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
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
					t.Error(err)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Error(err)
				}
				if resp.StatusCode != http.StatusOK {
					t.Errorf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
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
					t.Error(err)
				}
				zw.Close()
				req, err := http.NewRequest("POST", "http://127.0.0.1:9090", &buf)
				req.Header.Set("Content-Encoding", "gzip")
				if err != nil {
					t.Error(err)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Error(err)
				}
				if resp.StatusCode != http.StatusBadGateway {
					t.Errorf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusBadGateway)
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
					t.Errorf("unexpected erorr: %s", err)
				}
				if resp.StatusCode != http.StatusForbidden {
					t.Errorf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
				}
				response, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					t.Errorf("unexpected err while reading body: %s", err)
				}
				stringResponse := string(response)
				expErr := "user \"default\" is not allowed to access via http"
				if stringResponse[35:] != expErr {
					t.Errorf("unexpected response: %q; expected: %q", string(response), expErr)
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
					t.Errorf("unexpected erorr: %s", err)
				}
				if resp.StatusCode != http.StatusForbidden {
					t.Errorf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
				}
				response, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					t.Errorf("unexpected err while reading body: %s", err)
				}
				res := string(response)
				res = res[0:47]
				expErr := "http connections are not allowed from 127.0.0.1"
				if res != expErr {
					t.Errorf("unexpected response: %q; expected: %q", res, expErr)
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
					t.Errorf("unexpected erorr: %s", err)
				}
				if resp.StatusCode != http.StatusForbidden {
					t.Errorf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
				}
				response, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					t.Errorf("unexpected err while reading body: %s", err)
				}
				res := string(response)
				res = res[0:54]
				expErr := "connections to /metrics are not allowed from 127.0.0.1"
				if res != expErr {
					t.Errorf("unexpected response: %q; expected: %q", res, expErr)
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
					t.Errorf("unexpected erorr: %s", err)
				}
				if resp.StatusCode != http.StatusForbidden {
					t.Errorf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
				}
				response, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					t.Errorf("unexpected err while reading body: %s", err)
				}
				res := string(response)
				res = res[35:]
				expErr := "user \"default\" is not allowed to access"
				if res != expErr {
					t.Errorf("unexpected response: %q; expected: %q", res, expErr)
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
				res = res[35:]
				expErr := "cluster user \"web\" is not allowed to access"
				if res != expErr {
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
			time.Sleep(time.Millisecond * 10)
			tc.testFn(t)
			if err := ln.Close(); err != nil {
				t.Fatalf("unexpected error while closing listener: %s", err)
			}
			<-done
		})
	}
}
