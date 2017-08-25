package main

import (
	"fmt"
	"math/rand"
	"net/url"
	"sync"
	"time"

	"github.com/hagen1778/chproxy/log"
)

func (s *scope) String() string {
	return fmt.Sprintf("[ Id: %d; User %q(%d) proxying as %q(%d) to %q(%d) ]",
		s.id,
		s.initialUser.name, s.initialUser.runningQueries(),
		s.executionUser.name, s.executionUser.runningQueries(),
		s.host.addr.Host, s.host.runningQueries())
}

// TODO: rethink scope because it looks weird
type scope struct {
	id            uint64
	initialUser   *initialUser
	executionUser *executionUser
	cluster       *cluster
	host          *host
}

var r = rand.New(rand.NewSource(time.Now().UnixNano()))

func newScope(iu *initialUser, eu *executionUser, c *cluster) *scope {
	return &scope{
		id:            r.Uint64(),
		initialUser:   iu,
		executionUser: eu,
		cluster:       c,
		host:          c.getHost(),
	}
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
	toCluster, toUser string
	allowedIPs map[string]struct{}

	executionUser
}

type executionUser struct {
	name, password       string
	maxExecutionTime     time.Duration
	maxConcurrentQueries uint32

	queryCounter
}

func (u *executionUser) inc() error {
	if u.maxConcurrentQueries > 0 && u.runningQueries() >= u.maxConcurrentQueries {
		return fmt.Errorf("maxConcurrentQueries limit: %d", u.maxConcurrentQueries)
	}

	u.queryCounter.inc()
	return nil
}

func (u *executionUser) timeout() <-chan time.Time {
	if u.maxExecutionTime > 0 {
		return time.After(u.maxExecutionTime)
	}

	return nil
}

type host struct {
	addr *url.URL

	queryCounter
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
		if t.runningQueries() == 0 {
			return t
		}

		if idle == nil || idle.runningQueries() > t.runningQueries() {
			idle = t
		}
	}

	return idle
}

type queryCounter struct {
	sync.Mutex
	value uint32
}

func (qc *queryCounter) runningQueries() uint32 {
	qc.Lock()
	defer qc.Unlock()

	return qc.value
}

func (qc *queryCounter) inc() {
	qc.Lock()
	qc.value++
	qc.Unlock()
}

func (qc *queryCounter) dec() {
	qc.Lock()
	qc.value--
	qc.Unlock()
}
