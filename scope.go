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

var r = rand.New(rand.NewSource(time.Now().UnixNano()))

func newScope(iu *user, eu *clusterUser, c *cluster) *scope {
	return &scope{
		id:          r.Uint64(),
		host:        c.getHost(),
		cluster:     c,
		user:        iu,
		clusterUser: eu,
	}
}

func (s *scope) inc() error {
	if err := s.user.inc(); err != nil {
		return fmt.Errorf("limits for user %q are exceeded: %s", s.user.name, err)
	}

	if err := s.clusterUser.inc(); err != nil {
		return fmt.Errorf("limits for cluster user %q are exceeded: %s", s.clusterUser.name, err)
	}

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

func (u *clusterUser) inc() error {
	if u.maxConcurrentQueries > 0 && u.runningQueries() >= u.maxConcurrentQueries {
		return fmt.Errorf("maxConcurrentQueries limit: %d", u.maxConcurrentQueries)
	}

	u.queryCounter.inc()
	return nil
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
