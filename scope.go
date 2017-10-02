package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Vertamedia/chproxy/config"
	"github.com/Vertamedia/chproxy/log"
	"github.com/prometheus/client_golang/prometheus"
	"net"
)

func (s *scope) String() string {
	return fmt.Sprintf("[ Id: %d; User %q(%d) proxying as %q(%d) to %q(%d) ]",
		s.id,
		s.user.name, s.user.queryCounter.load(),
		s.clusterUser.name, s.clusterUser.queryCounter.load(),
		s.host.addr.Host, s.host.load())
}

type scope struct {
	id          uint32
	host        *host
	cluster     *cluster
	user        *user
	clusterUser *clusterUser
}

var scopeID = uint32(time.Now().UnixNano())

func newScope(u *user, cu *clusterUser, c *cluster) (*scope, error) {
	h := c.getHost()
	if h == nil {
		return nil, fmt.Errorf("no active hosts")
	}
	return &scope{
		id:          atomic.AddUint32(&scopeID, 1),
		host:        h,
		cluster:     c,
		user:        u,
		clusterUser: cu,
	}, nil
}

func (s *scope) inc() error {
	uQueries := s.user.queryCounter.inc()
	cQueries := s.clusterUser.queryCounter.inc()
	uRateLimit := s.user.rateLimiter.inc()
	cRateLimit := s.clusterUser.rateLimiter.inc()
	s.host.inc()

	var err error
	if s.user.maxConcurrentQueries > 0 && uQueries > s.user.maxConcurrentQueries {
		err = fmt.Errorf("limits for user %q are exceeded: max_concurrent_queries limit: %d",
			s.user.name, s.user.maxConcurrentQueries)
	}
	if s.clusterUser.maxConcurrentQueries > 0 && cQueries > s.clusterUser.maxConcurrentQueries {
		err = fmt.Errorf("limits for cluster user %q are exceeded: max_concurrent_queries limit: %d",
			s.clusterUser.name, s.clusterUser.maxConcurrentQueries)
	}
	if s.user.reqPerMin > 0 && uRateLimit > s.user.reqPerMin {
		err = fmt.Errorf("rate limit for user %q is exceeded: requests_per_minute limit: %d",
			s.user.name, s.user.reqPerMin)
	}
	if s.clusterUser.reqPerMin > 0 && cRateLimit > s.clusterUser.reqPerMin {
		err = fmt.Errorf("rate limit for cluster user %q is exceeded: requests_per_minute limit: %d",
			s.clusterUser.name, s.clusterUser.reqPerMin)
	}
	if err != nil {
		s.dec()
		return err
	}

	return nil
}

// decrement only queryCounter because rateLimiter will be reset automatically
// and to avoid situation when rateLimiter will be reset before decrement,
// which would lead to negative values, rateLimiter will be increased for every request
// even if maxConcurrentQueries already generated an error
// also it allows to show a real amount of requests by `requests_per_minute` metric
func (s *scope) dec() {
	s.host.dec()
	s.user.queryCounter.dec()
	s.clusterUser.queryCounter.dec()
}

const killQueryTimeout = time.Second * 30

func (s *scope) killQuery() error {
	if len(s.cluster.killQueryUserName) == 0 {
		return nil
	}
	query := fmt.Sprintf("KILL QUERY WHERE query_id = '%d'", s.id)
	log.Debugf("ExecutionTime exceeded. Going to call query %q", query)

	r := strings.NewReader(query)
	addr := s.host.addr.String()
	req, err := http.NewRequest("POST", addr, r)
	if err != nil {
		return fmt.Errorf("error while creating kill query request to %s: %s", addr, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), killQueryTimeout)
	defer cancel()

	req = req.WithContext(ctx)

	// send request as kill_query_user
	req.SetBasicAuth(s.cluster.killQueryUserName, s.cluster.killQueryUserPassword)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("error while executing clickhouse query %q at %q: %s", query, addr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		responseBody, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code returned from query %q at %q: %d. Response body: %q",
			query, addr, resp.StatusCode, responseBody)
	}

	log.Debugf("Query with id=%d successfully killed", s.id)
	return nil
}

func (s *scope) decorateRequest(req *http.Request) *http.Request {
	// make new params to purify URL
	params := make(url.Values)

	// set query_id as scope_id to have possibility kill query if needed
	params.Set("query_id", strconv.Itoa(int(s.id)))
	// if query was passed - keep it
	q := req.URL.Query().Get("query")
	if len(q) > 0 {
		params.Set("query", q)
	}
	req.URL.RawQuery = params.Encode()

	// rewrite possible previous Basic Auth
	// and send request as cluster user
	req.SetBasicAuth(s.clusterUser.name, s.clusterUser.password)

	// send request to chosen host from cluster
	req.URL.Scheme = s.host.addr.Scheme
	req.URL.Host = s.host.addr.Host

	// extend ua with additional info
	localAddr := "unknown"
	if addr, ok := req.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		localAddr = addr.String()
	}
	ua := fmt.Sprintf("RemoteAddr: %s; LocalAddr: %s; CHProxy-User: %s; CHProxy-ClusterUser: %s; %s",
		req.RemoteAddr, localAddr, s.user.name, s.clusterUser.name, req.UserAgent())
	req.Header.Set("User-Agent", ua)
	return req
}

type user struct {
	toUser          string
	toCluster       string
	denyHTTP        bool
	denyHTTPS       bool
	allowedNetworks config.Networks

	name, password       string
	maxExecutionTime     time.Duration
	maxConcurrentQueries uint32
	reqPerMin            uint32

	rateLimiter  rateLimiter
	queryCounter counter
}

type clusterUser struct {
	name, password       string
	maxExecutionTime     time.Duration
	maxConcurrentQueries uint32
	reqPerMin            uint32

	rateLimiter  rateLimiter
	queryCounter counter
}

type host struct {
	// counter of unsuccessful requests to decrease
	// host priority
	penalty uint32
	// if equal to 0 then this obj wouldn't be returned from getHost()
	active uint32
	// host address
	addr *url.URL

	counter
}

func (h *host) runHeartbeat(interval time.Duration, label prometheus.Labels, done <-chan struct{}) {
	heartbeat := func() {
		if err := isHealthy(h.addr.String()); err == nil {
			atomic.StoreUint32(&h.active, uint32(1))
			hostHealth.With(label).Set(1)
		} else {
			log.Errorf("error while health-checking %q host: %s", h.addr.Host, err)
			atomic.StoreUint32(&h.active, uint32(0))
			hostHealth.With(label).Set(0)
		}
	}
	heartbeat()
	for {
		select {
		case <-done:
			return
		case <-time.After(interval):
			heartbeat()
		}
	}
}

func (h *host) isActive() bool {
	return atomic.LoadUint32(&h.active) == 1
}

const (
	// prevents excess goroutine creating while penalizing overloaded host
	penaltySize     = 5
	penaltyMaxSize  = 300
	penaltyDuration = time.Second * 10
)

// decrease host priority for next requests
func (h *host) penalize() {
	p := atomic.LoadUint32(&h.penalty)
	if p >= penaltyMaxSize {
		return
	}
	log.Debugf("Penalizing host %q", h.addr)
	hostPenalties.With(prometheus.Labels{"host": h.addr.Host}).Inc()
	atomic.AddUint32(&h.penalty, penaltySize)
	time.AfterFunc(penaltyDuration, func() {
		atomic.AddUint32(&h.penalty, ^uint32(penaltySize-1))
	})
}

// overload runningQueries to take penalty into consideration
func (h *host) load() uint32 {
	c := h.counter.load()
	p := atomic.LoadUint32(&h.penalty)
	return c + p
}

type cluster struct {
	nextIdx               uint32
	hosts                 []*host
	users                 map[string]*clusterUser
	killQueryUserName     string
	killQueryUserPassword string
	heartBeatInterval     time.Duration
}

// get least loaded + round-robin host from cluster
func (c *cluster) getHost() *host {
	idx := atomic.AddUint32(&c.nextIdx, 1)
	l := uint32(len(c.hosts))
	idx = idx % l
	idle := c.hosts[idx]
	idleN := idle.load()

	if idleN == 0 && idle.isActive() {
		return idle
	}

	// round hosts checking
	// until the least loaded is found
	for i := (idx + 1) % l; i != idx; i = (i + 1) % l {
		h := c.hosts[i]
		if !h.isActive() {
			continue
		}
		n := h.load()
		if n == 0 {
			return h
		}
		if n < idleN {
			idle, idleN = h, n
		}
	}
	if !idle.isActive() {
		return nil
	}
	return idle
}

type rateLimiter struct {
	counter
}

func (rl *rateLimiter) run(label prometheus.Labels, done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case <-time.After(time.Minute):
			v := atomic.LoadUint32(&rl.value)
			requestPerMin.With(label).Set(float64(v))
			atomic.StoreUint32(&rl.value, 0)
		}
	}
}

type counter struct {
	value uint32
}

func (c *counter) load() uint32 { return atomic.LoadUint32(&c.value) }

func (c *counter) dec() { atomic.AddUint32(&c.value, ^uint32(0)) }

func (c *counter) inc() uint32 { return atomic.AddUint32(&c.value, 1) }
