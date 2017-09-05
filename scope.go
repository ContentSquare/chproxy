package main

import (
	"fmt"
	"math/rand"
	"net/url"
	"sync"
	"time"

	"github.com/hagen1778/chproxy/config"
	"github.com/hagen1778/chproxy/log"
)

func (s *scope) String() string {
	return fmt.Sprintf("[ Id: %d; User %q(%d) proxying as %q(%d) to %q(%d) ]",
		s.id,
		s.user.name, s.user.runningQueries(),
		s.clusterUser.name, s.clusterUser.runningQueries(),
		s.host.addr.Host, s.host.runningQueries())
}

// TODO: rethink scope because it looks weird
type scope struct {
	id          uint64
	host        *host
	cluster     *cluster
	user        *user
	clusterUser *clusterUser
}

func newScope(iu *user, eu *clusterUser, c *cluster) *scope {
	return &scope{
		id:          rand.Uint64(),
		host:        c.getHost(),
		cluster:     c,
		user:        iu,
		clusterUser: eu,
	}
}

func (s *scope) inc() error {
	if s.user.maxConcurrentQueries > 0 && s.user.runningQueries() >= s.user.maxConcurrentQueries {
		return fmt.Errorf("limits for user %q are exceeded: maxConcurrentQueries limit: %d", s.user.name, s.user.maxConcurrentQueries)
	}

	if s.clusterUser.maxConcurrentQueries > 0 && s.clusterUser.runningQueries() >= s.clusterUser.maxConcurrentQueries {
		return fmt.Errorf("limits for cluster user %q are exceeded: maxConcurrentQueries limit: %d", s.clusterUser.name, s.clusterUser.maxConcurrentQueries)
	}

	s.user.inc()
	s.clusterUser.inc()
	s.host.inc()
	return nil
}

func (s *scope) dec() {
	s.host.dec()
	s.user.dec()
	s.clusterUser.dec()
}

type user struct {
	toUser          string
	toCluster       string
	allowedNetworks config.Networks

	clusterUser
}

type clusterUser struct {
	name, password       string
	maxExecutionTime     time.Duration
	maxConcurrentQueries uint32

	queryCounter
}

func (u *clusterUser) timeout() <-chan time.Time {
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
	nextIdx uint32
	hosts   []*host
	users   map[string]*clusterUser
}

func newCluster(h []*host, u map[string]*clusterUser) *cluster {
	return &cluster{
		hosts:   h,
		users:   u,
		nextIdx: uint32(time.Now().UnixNano()),
	}
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

// get least loaded + round-robin host from cluster
func (c *cluster) getHost() *host {
	c.Lock()
	defer c.Unlock()

	c.nextIdx++

	l := uint32(len(c.hosts))
	idx := c.nextIdx % l
	idle := c.hosts[idx]
	idleN := idle.runningQueries()

	if idleN == 0 {
		return idle
	}

	// round checking of hosts in slice
	// until hits least loaded
	for i := (idx + 1) % l; i != idx; i = (i + 1) % l {
		h := c.hosts[i]
		n := h.runningQueries()
		if n == 0 {
			return h
		}
		if n < idleN {
			idle, idleN = h, n
		}
	}

	return idle
}

type queryCounter struct {
	mu sync.Mutex
	value uint32
}

func (qc *queryCounter) runningQueries() uint32 {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	return qc.value
}

func (qc *queryCounter) inc() {
	qc.mu.Lock()
	qc.value++
	qc.mu.Unlock()
}

func (qc *queryCounter) dec() {
	qc.mu.Lock()
	qc.value--
	qc.mu.Unlock()
}
