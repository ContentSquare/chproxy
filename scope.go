package main

import (
	"fmt"
	"net/url"
	"sync"
	"time"
	"github.com/hagen1778/chproxy/log"
)

func (s *scope) String() string {
	return fmt.Sprintf("[ InitialUser %q(%d) matched to ExecutionUser %q(%d) => %q(%d) ]",
		s.initialUser.name, s.initialUser.runningQueries,
		s.executionUser.name, s.executionUser.runningQueries,
		s.host.addr.Host, s.host.runningQueries)
}

// TODO: rethink scope because it looks weird
type scope struct {
	initialUser   *initialUser
	executionUser *executionUser
	cluster       *cluster
	host          *host
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
	maxExecutionTime     time.Duration

	sync.Mutex
	maxConcurrentQueries uint32
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

func (u *executionUser) after() <- chan time.Time {
	if u.maxExecutionTime > 0 {
		return time.After(u.maxExecutionTime)
	}

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

type cluster struct {
	sync.Mutex
	hosts []*host
	users map[string]*executionUser
}

// We don't use query_id because of distributed processing, the query ID is not passed to remote servers
func (c *cluster) killQueries(condition string, elapsed float64) {
	c.Lock()
	addrs := make([]string, len(c.hosts))
	for i, host := range c.hosts {
		addrs[i] = host.addr.String()
	}
	c.Unlock()

	q := fmt.Sprintf("KILL QUERY WHERE %s AND elapsed >= %d", condition, int(elapsed))
	log.Debugf("ExecutionTime exceeded. Going to call query %q for hosts %v", q, addrs)
	for _, addr := range addrs {
		if err := doQuery(q, addr); err != nil {
			log.Errorf("error while killing queries older than %.2fs: %s", elapsed, err)
		}
	}
}

func (c *cluster) getHost() *host {
	c.Lock()
	defer c.Unlock()

	var idle *host
	for _, t := range c.hosts {
		t.Lock()
		if t.runningQueries == 0 {
			t.Unlock()
			return t
		}

		if idle == nil || idle.runningQueries > t.runningQueries {
			idle = t
		}
		t.Unlock()
	}

	return idle
}
