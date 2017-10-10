package main

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
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
				addr:   &url.URL{Host: "127.0.0.1"},
				active: 1,
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
	s := newScope()
	s.host = c.getHost()
	s.cluster = c
	s.user = u1
	s.clusterUser = cu
	s.labels = prometheus.Labels{
		"user":         "default",
		"cluster":      "default",
		"cluster_user": "default",
		"cluster_node": "default",
	}

	check := func(uq, cuq, hq uint32) {
		if s.user.queryCounter.load() != uq {
			t.Fatalf("expected runningQueries for user: %d; got: %d", uq, s.user.queryCounter.load())
		}

		if s.clusterUser.queryCounter.load() != cuq {
			t.Fatalf("expected runningQueries for cluster user: %d; got: %d", cuq, s.clusterUser.queryCounter.load())
		}

		if s.host.load() != hq {
			t.Fatalf("expected runningQueries for host: %d; got: %d", hq, s.host.load())
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
	s = newScope()
	s.host = c.getHost()
	s.cluster = c
	s.user = u2
	s.clusterUser = cu
	s.labels = prometheus.Labels{
		"user":         "default",
		"cluster":      "default",
		"cluster_user": "default",
		"cluster_node": "default",
	}
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
				addr:   &url.URL{Host: "127.0.0.1"},
				active: 1,
			},
			{
				addr:   &url.URL{Host: "127.0.0.2"},
				active: 1,
			},
			{
				addr:   &url.URL{Host: "127.0.0.3"},
				active: 1,
			},
		},
	}

	// step | expected  | hosts running queries
	// 1    | 127.0.0.2 | 0, 1, 0
	// 2    | 127.0.0.3 | 0, 1, 1
	// 3    | 127.0.0.1 | 1, 1, 1
	// 4    | 127.0.0.2 | 1, 2, 2
	// 5    | 127.0.0.1 | 2, 2, 2
	// 6    | 127.0.0.1 | 3, 7, 2	// 2nd is penalized for `penaltySize`
	// 7    | 127.0.0.1 | 3, 7, 3

	// step: 1
	h := c.getHost()
	expected := "127.0.0.2"
	if h.addr.Host != expected {
		t.Fatalf("got host %q; expected %q", h.addr.Host, expected)
	}
	h.inc()

	// step: 2
	h = c.getHost()
	expected = "127.0.0.3"
	if h.addr.Host != expected {
		t.Fatalf("got host %q; expected %q", h.addr.Host, expected)
	}
	h.inc()

	// step: 3
	h = c.getHost()
	expected = "127.0.0.1"
	if h.addr.Host != expected {
		t.Fatalf("got host %q; expected %q", h.addr.Host, expected)
	}
	h.inc()

	// step: 4
	h = c.getHost()
	expected = "127.0.0.2"
	if h.addr.Host != expected {
		t.Fatalf("got host %q; expected %q", h.addr.Host, expected)
	}
	h.inc()

	// inc last host to get least-loaded 1st host
	c.hosts[2].inc()

	// step: 5
	h = c.getHost()
	expected = "127.0.0.1"
	if h.addr.Host != expected {
		t.Fatalf("got host %q; expected %q", h.addr.Host, expected)
	}
	h.inc()

	// penalize 2nd host
	h = c.hosts[1]
	expRunningQueries := penaltySize + h.load()
	h.penalize()
	if h.load() != expRunningQueries {
		t.Fatalf("got host %q running queries %d; expected %d", h.addr.Host, h.load(), expRunningQueries)
	}

	// step: 6
	// we got "127.0.0.1" because index it's 6th step, hence index is = 0
	h = c.getHost()
	expected = "127.0.0.1"
	if h.addr.Host != expected {
		t.Fatalf("got host %q; expected %q", h.addr.Host, expected)
	}
	h.inc()

	// step: 7
	// we got "127.0.0.3"; index = 1, means to get 2nd host, but it has runningQueries=7
	// so we will get next least loaded
	h = c.getHost()
	expected = "127.0.0.3"
	if h.addr.Host != expected {
		t.Fatalf("got host %q; expected %q", h.addr.Host, expected)
	}
	h.inc()
}

func TestPenalize(t *testing.T) {
	h := &host{
		addr: &url.URL{Host: "127.0.0.1"},
	}
	exp := uint32(0)
	if h.load() != exp {
		t.Fatalf("got running queries %d; expected %d", h.load(), exp)
	}

	h.penalize()
	exp = uint32(penaltySize)
	if h.load() != exp {
		t.Fatalf("got running queries %d; expected %d", h.load(), exp)
	}

	// do more penalties than `penaltyMaxSize` allows
	c := int(penaltyMaxSize/penaltySize) * 2
	for i := 0; i < c; i++ {
		h.penalize()
	}
	exp = uint32(penaltyMaxSize)
	if h.load() != exp {
		t.Fatalf("got running queries %d; expected %d", h.load(), exp)
	}

	// but still might increased
	h.inc()
	exp++
	if h.load() != exp {
		t.Fatalf("got running queries %d; expected %d", h.load(), exp)
	}
}

func TestRunningQueriesConcurrent(t *testing.T) {
	eu := &clusterUser{
		maxConcurrentQueries: 10,
	}
	f := func() {
		eu.queryCounter.inc()
		eu.queryCounter.load()
		eu.queryCounter.dec()
	}
	if err := testConcurrent(f, 1000); err != nil {
		t.Fatalf("concurrent test err: %s", err)
	}
}

func TestGetHostConcurrent(t *testing.T) {
	c := &cluster{
		hosts: []*host{
			{
				addr:   &url.URL{Host: "127.0.0.1"},
				active: 1,
			},
			{
				addr:   &url.URL{Host: "127.0.0.2"},
				active: 1,
			},
			{
				addr:   &url.URL{Host: "127.0.0.3"},
				active: 1,
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
