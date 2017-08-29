package main

import (
	"net/url"
	"testing"
)

func TestScope_RunningQueries(t *testing.T) {
	eu := &clusterUser{
		maxConcurrentQueries: 1,
	}

	if err := eu.inc(); err != nil {
		t.Fatalf("unexpected err: %s", err)
	}

	if eu.runningQueries() != 1 {
		t.Fatalf("expected runningQueries: 1; got: %d", eu.runningQueries())
	}

	if err := eu.inc(); err == nil {
		t.Fatalf("error expected while call .inc()")
	}

	eu.dec()

	if err := eu.inc(); err != nil {
		t.Fatalf("unexpected err: %s", err)
	}
}

func TestScope_GetHost(t *testing.T) {
	c := &cluster{
		hosts: []*host{
			{
				addr: &url.URL{Host: "127.0.0.1"},
			},
			{
				addr: &url.URL{Host: "127.0.0.2"},
			},
			{
				addr: &url.URL{Host: "127.0.0.3"},
			},
		},
	}

	// step | expected  | hosts running queries
	// 0    | 127.0.0.2 | 0, 0, 0
	// 1    | 127.0.0.3 | 0, 0, 0
	// 2    | 127.0.0.1 | 0, 0, 0
	// 3    | 127.0.0.2 | 0, 0, 0
	// 4    | 127.0.0.1 | 0, 0, 1

	h := c.getHost()
	expected := "127.0.0.2"
	if h.addr.Host != expected {
		t.Fatalf("got host %q; expected %q", h.addr.Host, expected)
	}

	h = c.getHost()
	expected = "127.0.0.3"
	if h.addr.Host != expected {
		t.Fatalf("got host %q; expected %q", h.addr.Host, expected)
	}

	h = c.getHost()
	expected = "127.0.0.1"
	if h.addr.Host != expected {
		t.Fatalf("got host %q; expected %q", h.addr.Host, expected)
	}

	h = c.getHost()
	expected = "127.0.0.2"
	if h.addr.Host != expected {
		t.Fatalf("got host %q; expected %q", h.addr.Host, expected)
	}

	c.hosts[2].inc()
	h = c.getHost()
	expected = "127.0.0.1"
	if h.addr.Host != expected {
		t.Fatalf("got host %q; expected %q", h.addr.Host, expected)
	}
}
