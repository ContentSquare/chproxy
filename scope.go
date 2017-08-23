package main

import (
	"fmt"
	"net/url"
	"sync"
	"time"
)

type scope struct {
	initialUser   *initialUser
	executionUser *executionUser
	cluster       *cluster
	host          *host
}

func (s *scope) String() string {
	return fmt.Sprintf("[ InitialUser %q(%d) matched to ExecutionUser %q(%d) => %q(%d) ]",
		s.initialUser.name, s.initialUser.runningQueries,
		s.executionUser.name, s.executionUser.runningQueries,
		s.host.addr.Host, s.host.runningQueries)
}

func (s *scope) inc() error {
	if err := s.initialUser.inc(); err != nil {
		return fmt.Errorf("limits for initial user %q are exceeded: %s", s.initialUser.name, err)
	}

	if err := s.executionUser.inc(); err != nil {
		return fmt.Errorf("limits for execution user %q are exceeded: %s", s.executionUser.name, err)
	}

	s.host.inc()
	return nil
}

func (s *scope) dec() {
	s.initialUser.dec()
	s.executionUser.dec()
	s.host.dec()
}

type initialUser struct {
	executionUser

	toCluster, toUser string
}

type executionUser struct {
	name, password string

	sync.Mutex
	maxConcurrentQueries uint32
	maxExecutionTime     time.Duration
	runningQueries       uint32
}

func (u *executionUser) inc() error {
	u.Lock()
	defer u.Unlock()

	if u.maxConcurrentQueries > 0 && u.runningQueries >= u.maxConcurrentQueries {
		return fmt.Errorf("maxConcurrentQueries limit exceeded: %d", u.maxConcurrentQueries)
	}

	u.runningQueries++
	return nil
}

func (u *executionUser) dec() {
	u.Lock()
	u.runningQueries--
	u.Unlock()
}

type host struct {
	addr *url.URL

	sync.Mutex
	runningQueries uint32
}

func (t *host) inc() {
	t.Lock()
	t.runningQueries++
	t.Unlock()
}

func (t *host) dec() {
	t.Lock()
	t.runningQueries--
	t.Unlock()
}
