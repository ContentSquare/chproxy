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

	go serveTLS(cfg.ListenAddr, cfg.TLSConfig)
	time.Sleep(time.Second)

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
}

func TestServe(t *testing.T) {
	*configFile = "testdata/http.conf.yml"
	cfg, err := reloadConfig()
	if err != nil {
		t.Fatalf("unexpected error while loading config: %s", err)
	}
	go serve(cfg.ListenAddr)
	time.Sleep(time.Second)

	resp, err := http.Get("http://127.0.0.1:8080/metrics")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: %d; expected: %d", resp.StatusCode, http.StatusOK)
	}
}
