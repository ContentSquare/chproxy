package main

import (
	"fmt"
	"net/url"
	"testing"
	"time"
)

var (
	cu = &clusterUser{
		maxConcurrentQueries: 2,
	}

	c = &cluster{
		hosts: []*host{
			{
				addr: &url.URL{Host: "127.0.0.1"},
			},
		},
		users: map[string]*clusterUser{
			"cu": cu,
		},
		nextIdx: uint32(time.Now().UnixNano()),
	}
)

func TestRunningQueries(t *testing.T) {
	u1 := &user{
		maxConcurrentQueries: 1,
	}
	s := newScope(u1, cu, c)

	check := func(uq, cuq, hq uint32) {
		if s.user.runningQueries() != uq {
			t.Fatalf("expected runningQueries for user: %d; got: %d", uq, s.user.runningQueries())
		}

		if s.clusterUser.runningQueries() != cuq {
			t.Fatalf("expected runningQueries for cluster user: %d; got: %d", cuq, s.clusterUser.runningQueries())
		}

		if s.host.runningQueries() != hq {
			t.Fatalf("expected runningQueries for host: %d; got: %d", hq, s.host.runningQueries())
		}
	}

	// initial check
	check(0, 0, 0)

	if err := s.inc(); err != nil {
		t.Fatalf("unexpected err: %s", err)
	}
	// check after first increase
	check(1, 1, 1)

	// next inc expected to hit limits
	if err := s.inc(); err == nil {
		t.Fatalf("error expected while call .inc()")
	}
	// check that limits are still same after error
	check(1, 1, 1)

	u2 := &user{
		maxConcurrentQueries: 1,
	}
	s = newScope(u2, cu, c)
	if err := s.inc(); err != nil {
		t.Fatalf("unexpected err: %s", err)
	}
	// inc with different user, but with the same cluster
	check(1, 2, 2)

	s.dec()
	check(0, 1, 1)
	if err := s.inc(); err != nil {
		t.Fatalf("unexpected err: %s", err)
	}

	check(1, 2, 2)
}

func TestGetHost(t *testing.T) {
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

func TestRunningQueriesConcurrent(t *testing.T) {
	eu := &clusterUser{
		maxConcurrentQueries: 10,
	}

	f := func() {
		eu.inc()
		eu.runningQueries()
		eu.dec()
	}
	if err := testConcurrent(f, 1000); err != nil {
		t.Fatalf("concurrent test err: %s", err)
	}
}

func TestGetHostConcurrent(t *testing.T) {
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

	f := func() {
		h := c.getHost()
		h.inc()
		h.dec()
	}
	if err := testConcurrent(f, 1000); err != nil {
		t.Fatalf("concurrent test err: %s", err)
	}
}

func testConcurrent(f func(), concurrency int) error {
	ch := make(chan struct{}, concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			f()
			ch <- struct{}{}
		}()
	}
	for i := 0; i < concurrency; i++ {
		select {
		case <-ch:
		case <-time.After(time.Second):
			return fmt.Errorf("timeout")
		}
	}
	return nil
}
