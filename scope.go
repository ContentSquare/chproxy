package main

import (
	"fmt"
	"net/url"
	"sync"
	"time"
)

type scope struct {
	user *user
	host *host
}

func (s *scope) String() string {
	return fmt.Sprintf("[User: %s, running queries: %d => Host: %s, running queries: %d]",
		s.user.name, s.user.runningQueries,
		s.host.addr.Host, s.host.runningQueries)
}

func (s *scope) inc() error {
	if err := s.user.Inc(); err != nil {
		return fmt.Errorf("limits for user %q are exceeded: %s", s.user.name, err)
	}
	s.host.Inc()
	return nil
}

func (s *scope) dec() {
	s.user.Dec()
	s.host.Dec()
}

type user struct {
	name string

	sync.Mutex
	maxConcurrentQueries uint32
	maxExecutionTime     time.Duration
	runningQueries       uint32
}

func (u *user) Inc() error {
	u.Lock()
	defer u.Unlock()

	if u.maxConcurrentQueries > 0 && u.runningQueries >= u.maxConcurrentQueries {
		return fmt.Errorf("maxConcurrentQueries limit exceeded: %d", u.maxConcurrentQueries)
	}

	u.runningQueries++
	return nil
}

func (u *user) Dec() {
	u.Lock()
	u.runningQueries--
	u.Unlock()
}

type host struct {
	addr *url.URL

	sync.Mutex
	runningQueries uint32
}

func (t *host) Inc() {
	t.Lock()
	t.runningQueries++
	t.Unlock()
}

func (t *host) Dec() {
	t.Lock()
	t.runningQueries--
	t.Unlock()
}
