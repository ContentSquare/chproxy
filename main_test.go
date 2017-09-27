package main

import (
	"crypto/tls"
	"github.com/Vertamedia/chproxy/config"
	"net/http"
	"testing"
)

func TestServeTLS(t *testing.T) {
	*configFile = "testdata/tls.conf.yml"
	cfg, err := reloadConfig()
	if err != nil {
		t.Fatalf("unexpected error while loading config: %s", err)
	}
	done := make(chan struct{})
	an := func() *config.Networks { return allowedNetworksHTTPS.Load().(*config.Networks) }
	authLn := newAuthListener(cfg.HTTPS.ListenAddr, an)
	tlsCfg := newTLSConfig(cfg.HTTPS)
	ln := tls.NewListener(authLn, tlsCfg)
	go func() {
		listenAndServe(ln)
		close(done)
	}()

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	req, err := http.NewRequest("GET", "https://127.0.0.1:8443/metrics", nil)
	if err != nil {
		t.Fatalf("unexpected erorr: %s", err)
	}

	req.SetBasicAuth("default", "qwerty")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("unexpected error while closing listener: %s", err)
	}
	<-done
}

func TestServe(t *testing.T) {
	*configFile = "testdata/http.conf.yml"
	cfg, err := reloadConfig()
	if err != nil {
		t.Fatalf("unexpected error while loading config: %s", err)
	}
	done := make(chan struct{})
	an := func() *config.Networks {
		return allowedNetworksHTTP.Load().(*config.Networks)
	}
	ln := newAuthListener(cfg.HTTP.ListenAddr, an)
	go func() {
		listenAndServe(ln)
		close(done)
	}()

	resp, err := http.Get("http://127.0.0.1:9090/metrics")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("unexpected error while closing listener: %s", err)
	}
	<-done
}
