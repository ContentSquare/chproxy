package main

import (
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
)

func (s *scope) String() string {
	return fmt.Sprintf("[ Id: %d; User %q(%d) proxying as %q(%d) to %q(%d) ]",
		s.id,
		s.user.name, s.user.runningQueries(),
		s.clusterUser.name, s.clusterUser.runningQueries(),
		s.host.addr.Host, s.host.runningQueries())
}

type scope struct {
	id          uint32
	host        *host
	cluster     *cluster
	user        *user
	clusterUser *clusterUser
}

var scopeId = uint32(time.Now().UnixNano())

func newScope(u *user, cu *clusterUser, c *cluster) *scope {
	return &scope{
		id:          atomic.AddUint32(&scopeId, 1),
		host:        c.getHost(),
		cluster:     c,
		user:        u,
		clusterUser: cu,
	}
}

func (s *scope) inc() error {
	uq := s.user.inc()
	cq := s.clusterUser.inc()
	s.host.inc()

	var err error
	if s.user.maxConcurrentQueries > 0 && uq > s.user.maxConcurrentQueries {
		err = fmt.Errorf("limits for user %q are exceeded: maxConcurrentQueries limit: %d", s.user.name, s.user.maxConcurrentQueries)
	}

	if s.clusterUser.maxConcurrentQueries > 0 && cq > s.clusterUser.maxConcurrentQueries {
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

	// send request as kill_query_user
	req.SetBasicAuth(s.cluster.killQueryUserName, s.cluster.killQueryUserPassword)

	resp, err := client.Do(req)
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
	params := req.URL.Query()

	// delete possible credentials from query since they
	// aren't longer needed
	params.Del("user")
	params.Del("password")

	// set query_id as scope_id to have possibility kill query if needed
	params.Set("query_id", strconv.Itoa(int(s.id)))
	req.URL.RawQuery = params.Encode()

	// rewrite possible previous Basic Auth
	// and send request as cluster user
	req.SetBasicAuth(s.clusterUser.name, s.clusterUser.password)

	// send request to chosen host from cluster
	req.URL.Scheme = s.host.addr.Scheme
	req.URL.Host = s.host.addr.Host

	// extend ua with additional info
	ua := fmt.Sprintf("IP: %s; CHProxy-User: %s; CHProxy-ClusterUser: %s; %s",
		req.RemoteAddr, s.user.name, s.clusterUser.name, req.UserAgent())
	req.Header.Set("User-Agent", ua)

	return req
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

type host struct {
	// counter of unsuccessful requests to decrease
	// host priority
	penalty uint32

	addr *url.URL

	queryCounter
}

const (
	penaltyDuration = time.Minute
	penaltySize     = 5
	penaltyMaxSize  = 300
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
func (h *host) runningQueries() uint32 {
	c := h.queryCounter.runningQueries()
	p := atomic.LoadUint32(&h.penalty)
	return c + p
}

type cluster struct {
	nextIdx               uint32
	hosts                 []*host
	users                 map[string]*clusterUser
	killQueryUserName     string
	killQueryUserPassword string
}

var client = &http.Client{
	Timeout: time.Second * 60,
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

func (qc *queryCounter) inc() uint32 {
	return atomic.AddUint32(&qc.value, 1)
}

func (qc *queryCounter) dec() {
	atomic.AddUint32(&qc.value, ^uint32(0))
}
