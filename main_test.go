package main

import (
	"crypto/tls"
	"net/http"
	"testing"
	"time"
)

func TestServeTLS(t *testing.T) {
	*configFile = "testdata/tls.conf.yml"
	cfg, err := reloadConfig()
	if err != nil {
		t.Fatalf("unexpected error while loading config: %s", err)
	}

	done := make(chan error)
	ln := newTLSListener(cfg.ListenAddr, &cfg.TLSConfig)
	go func(){
		done <- listenAndServe(ln)
	}()
	time.Sleep(time.Millisecond*200)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	resp, err := client.Get("https://127.0.0.1:9090/metrics")
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

	done := make(chan error)
	ln := newListener(cfg.ListenAddr)
	go func(){
		done <- listenAndServe(ln)
	}()
	time.Sleep(time.Millisecond*200)

	resp, err := http.Get("http://127.0.0.1:8080/metrics")
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
