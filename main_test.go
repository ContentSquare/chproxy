package main

import (
	"crypto/tls"
	"net/http"
	"testing"
	"net"
)

func TestServeTLS(t *testing.T) {
	*configFile = "testdata/tls.conf.yml"
	cfg, err := reloadConfig()
	if err != nil {
		t.Fatalf("unexpected error while loading config: %s", err)
	}
	done := make(chan struct{})

	ln, err := net.Listen("tcp4", cfg.HTTPS.ListenAddr)
	if err != nil {
		t.Fatalf("cannot listen for %q: %s", cfg.HTTPS.ListenAddr, err)
	}
	tlsCfg := newTLSConfig(cfg.HTTPS)
	tln := tls.NewListener(ln, tlsCfg)
	go func() {
		listenAndServe(tln)
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
	if err := tln.Close(); err != nil {
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

	ln, err := net.Listen("tcp4", cfg.HTTP.ListenAddr)
	if err != nil {
		t.Fatalf("cannot listen for %q: %s", cfg.HTTP.ListenAddr, err)
	}
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

func TestServeMetrics(t *testing.T) {
	*configFile = "testdata/http.metrics.conf.yml"
	cfg, err := reloadConfig()
	if err != nil {
		t.Fatalf("unexpected error while loading config: %s", err)
	}
	done := make(chan struct{})

	ln, err := net.Listen("tcp4", cfg.HTTP.ListenAddr)
	if err != nil {
		t.Fatalf("cannot listen for %q: %s", cfg.HTTP.ListenAddr, err)
	}
	go func() {
		listenAndServe(ln)
		close(done)
	}()

	resp, err := http.Get("http://127.0.0.1:9091/metrics")
	if err != nil {
		t.Fatalf("expected error: %s", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusForbidden)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("unexpected error while closing listener: %s", err)
	}
	<-done
}
