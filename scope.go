package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Vertamedia/chproxy/config"
	"github.com/Vertamedia/chproxy/log"
	"github.com/prometheus/client_golang/prometheus"
)

func (s *scope) String() string {
	return fmt.Sprintf("[ Id: %X; User %q(%d) proxying as %q(%d) to %q(%d); RemoteAddr: %q; LocalAddr: %q ]",
		s.id,
		s.user.name, s.user.queryCounter.load(),
		s.clusterUser.name, s.clusterUser.queryCounter.load(),
		s.host.addr.Host, s.host.load(),
		s.remoteAddr, s.localAddr)
}

type scope struct {
	id          uint64
	host        *host
	cluster     *cluster
	user        *user
	clusterUser *clusterUser

	remoteAddr string
	localAddr  string

	labels prometheus.Labels
}

var scopeID = uint64(time.Now().UnixNano())

func (s *scope) incQueued() error {
	if s.user.queueCh == nil && s.clusterUser.queueCh == nil {
		// Request queues in the current scope are disabled.
		return s.inc()
	}

	// Do not store `cluster_node` in lables, since it has no sense
	// for queue metrics.
	labels := prometheus.Labels{
		"user":         s.labels["user"],
		"cluster":      s.labels["cluster"],
		"cluster_user": s.labels["cluster_user"],
	}

	if s.user.queueCh != nil {
		select {
		case s.user.queueCh <- struct{}{}:
			defer func() {
				<-s.user.queueCh
			}()
		default:
			// Per-user request queue is full.
			// Give the request the last chance to run.
			err := s.inc()
			if err != nil {
				userQueueOverflow.With(labels).Inc()
			}
			return err
		}
	}

	if s.clusterUser.queueCh != nil {
		select {
		case s.clusterUser.queueCh <- struct{}{}:
			defer func() {
				<-s.clusterUser.queueCh
			}()
		default:
			// Per-clusterUser request queue is full.
			// Give the request the last chance to run.
			err := s.inc()
			if err != nil {
				clusterUserQueueOverflow.With(labels).Inc()
			}
			return err
		}
	}

	// The request has been successfully queued.
	queueSize := requestQueueSize.With(labels)
	queueSize.Inc()
	defer queueSize.Dec()

	// Try starting the request during the given duration.
	d := s.maxQueueTime()
	dSleep := d / 10
	if dSleep > time.Second {
		dSleep = time.Second
	}
	if dSleep < time.Millisecond {
		dSleep = time.Millisecond
	}
	deadline := time.Now().Add(d)
	for {
		err := s.inc()
		if err == nil {
			// The request is allowed to start.
			return nil
		}

		dLeft := time.Until(deadline)
		if dLeft <= 0 {
			// Give up: the request exceeded its wait time
			// in the queue :(
			return err
		}

		// The request has dLeft remaining time to wait in the queue.
		// Sleep for a bit and try starting it again.
		if dSleep > dLeft {
			time.Sleep(dLeft)
		} else {
			time.Sleep(dSleep)
		}

		// Choose new host, since the previous one may become obsolete
		// after sleeping.
		h := s.cluster.getHost()
		if h != nil {
			s.host = h
			s.labels["cluster_node"] = h.addr.Host
		}
	}
}

func (s *scope) inc() error {
	uQueries := s.user.queryCounter.inc()
	cQueries := s.clusterUser.queryCounter.inc()

	var err error
	if s.user.maxConcurrentQueries > 0 && uQueries > s.user.maxConcurrentQueries {
		err = fmt.Errorf("limits for user %q are exceeded: max_concurrent_queries limit: %d",
			s.user.name, s.user.maxConcurrentQueries)
	}
	if s.clusterUser.maxConcurrentQueries > 0 && cQueries > s.clusterUser.maxConcurrentQueries {
		err = fmt.Errorf("limits for cluster user %q are exceeded: max_concurrent_queries limit: %d",
			s.clusterUser.name, s.clusterUser.maxConcurrentQueries)
	}

	uRPM := s.user.rateLimiter.inc()
	cRPM := s.clusterUser.rateLimiter.inc()

	// int32(xRPM) > 0 check is required to detect races when RPM
	// is decremented on error below after per-minute zeroing
	// in rateLimiter.run.
	// These races become innocent with the given check.
	if s.user.reqPerMin > 0 && int32(uRPM) > 0 && uRPM > s.user.reqPerMin {
		err = fmt.Errorf("rate limit for user %q is exceeded: requests_per_minute limit: %d",
			s.user.name, s.user.reqPerMin)
	}
	if s.clusterUser.reqPerMin > 0 && int32(cRPM) > 0 && cRPM > s.clusterUser.reqPerMin {
		err = fmt.Errorf("rate limit for cluster user %q is exceeded: requests_per_minute limit: %d",
			s.clusterUser.name, s.clusterUser.reqPerMin)
	}

	if err != nil {
		s.user.queryCounter.dec()
		s.clusterUser.queryCounter.dec()

		// Decrement rate limiter here, so it doesn't count requests
		// that didn't start due to limits overflow.
		s.user.rateLimiter.dec()
		s.clusterUser.rateLimiter.dec()
		return err
	}

	s.host.inc()
	concurrentQueries.With(s.labels).Inc()
	return nil
}

func (s *scope) dec() {
	// There is no need in ratelimiter.dec here, since the rate limiter
	// is automatically zeroed every minute in rateLimiter.run.

	s.user.queryCounter.dec()
	s.clusterUser.queryCounter.dec()
	s.host.dec()
	concurrentQueries.With(s.labels).Dec()
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

// allowedParams contains query args allowed to be proxed.
// See http://clickhouse-docs.readthedocs.io/en/latest/settings/
//
// All the other params passed via query args are stripped before
// proxying the request. This is for the sake of security.
var allowedParams = []string{
	"query",
	"database",
	"default_format",
	"readonly",
}

func (s *scope) decorateRequest(req *http.Request) *http.Request {
	// Make new params to purify URL.
	params := make(url.Values)

	// Set query_id as scope_id to have possibility to kill query if needed.
	params.Set("query_id", fmt.Sprintf("%X", s.id))

	// Keep allowed params.
	q := req.URL.Query()
	for _, param := range allowedParams {
		val := q.Get(param)
		if len(val) > 0 {
			params.Set(param, val)
		}
	}

	req.URL.RawQuery = params.Encode()

	// Rewrite possible previous Basic Auth and send request
	// as cluster user.
	req.SetBasicAuth(s.clusterUser.name, s.clusterUser.password)

	// Send request to the chosen host from cluster.
	req.URL.Scheme = s.host.addr.Scheme
	req.URL.Host = s.host.addr.Host

	// Extend ua with additional info, so it may be queried
	// via system.query_log.http_user_agent.
	ua := fmt.Sprintf("RemoteAddr: %s; LocalAddr: %s; CHProxy-User: %s; CHProxy-ClusterUser: %s; %s",
		s.remoteAddr, s.localAddr, s.user.name, s.clusterUser.name, req.UserAgent())
	req.Header.Set("User-Agent", ua)

	return req
}

func (s *scope) getTimeoutWithErrMsg() (time.Duration, error) {
	var (
		timeout       time.Duration
		timeoutErrMsg error
	)
	if s.user.maxExecutionTime > 0 {
		timeout = s.user.maxExecutionTime
		timeoutErrMsg = fmt.Errorf("timeout for user %q exceeded: %v", s.user.name, timeout)
	}
	if timeout == 0 || (s.clusterUser.maxExecutionTime > 0 && s.clusterUser.maxExecutionTime < timeout) {
		timeout = s.clusterUser.maxExecutionTime
		timeoutErrMsg = fmt.Errorf("timeout for cluster user %q exceeded: %v", s.clusterUser.name, timeout)
	}
	return timeout, timeoutErrMsg
}

func (s *scope) maxQueueTime() time.Duration {
	d := s.user.maxQueueTime
	if d <= 0 || s.clusterUser.maxQueueTime > 0 && s.clusterUser.maxQueueTime < d {
		d = s.clusterUser.maxQueueTime
	}
	if d <= 0 {
		// Default queue time.
		d = 10 * time.Second
	}
	return d
}

type user struct {
	toUser    string
	toCluster string
	denyHTTP  bool
	denyHTTPS bool
	allowCORS bool

	allowedNetworks config.Networks

	name, password       string
	maxExecutionTime     time.Duration
	maxConcurrentQueries uint32
	reqPerMin            uint32

	maxQueueTime time.Duration
	queueCh      chan struct{}

	rateLimiter  rateLimiter
	queryCounter counter
}

type clusterUser struct {
	allowedNetworks config.Networks

	name, password       string
	maxExecutionTime     time.Duration
	maxConcurrentQueries uint32
	reqPerMin            uint32

	maxQueueTime time.Duration
	queueCh      chan struct{}

	rateLimiter  rateLimiter
	queryCounter counter
}

type host struct {
	cluster *cluster

	// counter of unsuccessful requests to decrease
	// host priority
	penalty uint32
	// if equal to 0 then this obj wouldn't be returned from getHost()
	active uint32
	// host address
	addr *url.URL

	counter
}

func (h *host) runHeartbeat(done <-chan struct{}) {
	label := prometheus.Labels{
		"cluster":      h.cluster.name,
		"cluster_node": h.addr.Host,
	}
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
	interval := h.cluster.heartBeatInterval
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
	hostPenalties.With(prometheus.Labels{
		"cluster":      h.cluster.name,
		"cluster_node": h.addr.Host,
	}).Inc()
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
	name                  string
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

	// set least priority to inactive host
	if !idle.isActive() {
		idleN = ^uint32(0)
	}

	if idleN == 0 {
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

func (rl *rateLimiter) run(done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case <-time.After(time.Minute):
			rl.store(0)
		}
	}
}

type counter struct {
	value uint32
}

func (c *counter) store(n uint32) { atomic.StoreUint32(&c.value, n) }

func (c *counter) load() uint32 { return atomic.LoadUint32(&c.value) }

func (c *counter) dec() { atomic.AddUint32(&c.value, ^uint32(0)) }

func (c *counter) inc() uint32 { return atomic.AddUint32(&c.value, 1) }
