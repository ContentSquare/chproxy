package main

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"testing"
)

var tlsClient = &http.Client{Transport: &http.Transport{
	TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
}}

func startTLS() (net.Listener, chan struct{}) {
	cfg, err := reloadConfig()
	if err != nil {
		panic(fmt.Sprintf("unexpected error while loading config: %s", err))
	}
	done := make(chan struct{})

	ln, err := net.Listen("tcp4", cfg.HTTPS.ListenAddr)
	if err != nil {
		panic(fmt.Sprintf("cannot listen for %q: %s", cfg.HTTPS.ListenAddr, err))
	}
	tlsCfg := newTLSConfig(cfg.HTTPS)
	tln := tls.NewListener(ln, tlsCfg)
	go func() {
		listenAndServe(tln)
		close(done)
	}()
	return tln, done
}

func startHTTP() (net.Listener, chan struct{}) {
	cfg, err := reloadConfig()
	if err != nil {
		panic(fmt.Sprintf("unexpected error while loading config: %s", err))
	}
	done := make(chan struct{})

	ln, err := net.Listen("tcp4", cfg.HTTP.ListenAddr)
	if err != nil {
		panic(fmt.Sprintf("cannot listen for %q: %s", cfg.HTTP.ListenAddr, err))
	}
	go func() {
		listenAndServe(ln)
		close(done)
	}()
	return ln, done
}

func TestServe(t *testing.T) {

	var testCases = []struct {
		name   string
		file   string
		testFn func(t *testing.T)
		lnFn   func() (net.Listener, chan struct{})
	}{
		{
			"https request",
			"testdata/tls.conf.yml",
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
			"testdata/tls.conf.deny.https.yml",
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
				expErr := "user \"default\" is not allowed to access via https"
				if string(response) != expErr {
					t.Errorf("unexpected response: %q; expected: %q", string(response), expErr)
				}
				resp.Body.Close()
			},
			startTLS,
		},
		{
			"https networks",
			"testdata/tls.conf.networks.yml",
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
				res = res[0:48]
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
			"testdata/https.conf.user.networks.yml",
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
				res = res[0:54]
				expErr := "user \"default\" is not allowed to access from 127.0.0.1"
				if res != expErr {
					t.Errorf("unexpected response: %q; expected: %q", res, expErr)
				}
				resp.Body.Close()
			},
			startTLS,
		},
		{
			"https cluster user networks",
			"testdata/https.conf.cluster.user.networks.yml",
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
				res = res[0:58]
				expErr := "cluster user \"web\" is not allowed to access from 127.0.0.1"
				if res != expErr {
					t.Fatalf("unexpected response: %q; expected: %q", res, expErr)
				}
				resp.Body.Close()
			},
			startTLS,
		},
		{
			"http request",
			"testdata/http.conf.yml",
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
			"testdata/http.conf.deny.http.yml",
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
				expErr := "user \"default\" is not allowed to access via http"
				if string(response) != expErr {
					t.Errorf("unexpected response: %q; expected: %q", string(response), expErr)
				}
				resp.Body.Close()
			},
			startHTTP,
		},
		{
			"http networks",
			"testdata/http.conf.networks.yml",
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
			"testdata/http.conf.metrics.networks.yml",
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
			"testdata/http.conf.user.networks.yml",
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
				res = res[0:54]
				expErr := "user \"default\" is not allowed to access from 127.0.0.1"
				if res != expErr {
					t.Errorf("unexpected response: %q; expected: %q", res, expErr)
				}
				resp.Body.Close()
			},
			startHTTP,
		},
		{
			"http cluster user networks",
			"testdata/http.conf.cluster.user.networks.yml",
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
				res = res[0:58]
				expErr := "cluster user \"web\" is not allowed to access from 127.0.0.1"
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
			tl, done := tc.lnFn()
			tc.testFn(t)
			if err := tl.Close(); err != nil {
				t.Fatalf("unexpected error while closing listener: %s", err)
			}
			<-done
		})
	}
}
