package main

import (
	"fmt"
	"net/url"
	"sync/atomic"
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

type scope struct {
	id          int64
	host        *host
	cluster     *cluster
	user        *user
	clusterUser *clusterUser
}

var scopeId = time.Now().UnixNano()

func newScope(u *user, cu *clusterUser, c *cluster) *scope {
	return &scope{
		id:          atomic.AddInt64(&scopeId, 1),
		host:        c.getHost(),
		cluster:     c,
		user:        u,
		clusterUser: cu,
	}
}

func (s *scope) inc() error {
	s.user.inc()
	s.clusterUser.inc()
	s.host.inc()

	var err error
	if s.user.maxConcurrentQueries > 0 && s.user.runningQueries() > s.user.maxConcurrentQueries {
		err = fmt.Errorf("limits for user %q are exceeded: maxConcurrentQueries limit: %d", s.user.name, s.user.maxConcurrentQueries)
	}

	if s.clusterUser.maxConcurrentQueries > 0 && s.clusterUser.runningQueries() > s.clusterUser.maxConcurrentQueries {
		err = fmt.Errorf("limits for cluster user %q are exceeded: maxConcurrentQueries limit: %d", s.clusterUser.name, s.clusterUser.maxConcurrentQueries)
	}

	if err != nil {
		s.dec()
		return err
	}

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

	name, password       string
	maxExecutionTime     time.Duration
	maxConcurrentQueries uint32

	queryCounter
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
	nextIdx uint32
	hosts   []*host
	users   map[string]*clusterUser
}

func newCluster(h []*host, cu map[string]*clusterUser) *cluster {
	return &cluster{
		hosts:   h,
		users:   cu,
		nextIdx: uint32(time.Now().UnixNano()),
	}
}

// We don't use query_id because of distributed processing, the query ID is not passed to remote servers
func (c *cluster) killQueries(ua string, elapsed float64) {
	q := fmt.Sprintf("KILL QUERY WHERE http_user_agent = '%s' AND elapsed >= %d", ua, int(elapsed))
	log.Debugf("ExecutionTime exceeded. Going to call query %q for hosts %v", q, c.hosts)
	for _, host := range c.hosts {
		if err := doQuery(q, host.addr.String()); err != nil {
			log.Errorf("error while killing queries older than %.2fs: %s", elapsed, err)
		}
	}
}

// get least loaded + round-robin host from cluster
func (c *cluster) getHost() *host {
	idx := atomic.AddUint32(&c.nextIdx, 1)

	l := uint32(len(c.hosts))
	idx = idx % l
	idle := c.hosts[idx]
	idleN := idle.runningQueries()

	if idleN == 0 {
		return idle
	}

	// round hosts checking
	// until the least loaded is found
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
	value uint32
}

func (qc *queryCounter) runningQueries() uint32 {
	return atomic.LoadUint32(&qc.value)
}

func (qc *queryCounter) inc() {
	atomic.AddUint32(&qc.value, 1)
}

func (qc *queryCounter) dec() {
	atomic.AddUint32(&qc.value, ^uint32(0))
}
