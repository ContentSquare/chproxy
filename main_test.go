package main

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"testing"
	"time"
)

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
				if resp.StatusCode != http.StatusInternalServerError {
					t.Errorf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusInternalServerError)
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
				if resp.StatusCode != http.StatusInternalServerError {
					t.Errorf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusInternalServerError)
				}

				resp, err = http.Get("http://127.0.0.1:9090/metrics")
				if resp.StatusCode != http.StatusOK {
					t.Errorf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusInternalServerError)
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

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			*configFile = tc.file
			ln, done := tc.listenFn()
			tc.testFn(t)
			if err := ln.Close(); err != nil {
				t.Fatalf("unexpected error while closing listener: %s", err)
			}
			<-done
		})
	}
}
