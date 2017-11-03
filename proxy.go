package main

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Vertamedia/chproxy/cache"
	"github.com/Vertamedia/chproxy/config"
	"github.com/Vertamedia/chproxy/log"
	"github.com/prometheus/client_golang/prometheus"
)

type reverseProxy struct {
	*httputil.ReverseProxy

	reloadSignal chan struct{}
	reloadWG     sync.WaitGroup

	mu       sync.RWMutex
	users    map[string]*user
	clusters map[string]*cluster
}

func newReverseProxy() *reverseProxy {
	return &reverseProxy{
		ReverseProxy: &httputil.ReverseProxy{
			Director: func(*http.Request) {},

			// Suppress error logging in ReverseProxy, since all the errors
			// are handled and logged in the code below.
			ErrorLog: log.NilLogger,
		},
		reloadSignal: make(chan struct{}),
		reloadWG:     sync.WaitGroup{},
	}
}

func (rp *reverseProxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	timeStart := time.Now()

	s, status, err := rp.getScope(req)
	if err != nil {
		q := getQuerySnippet(req)
		err = fmt.Errorf("%q: %s; query: %q", req.RemoteAddr, err, q)
		respondWith(rw, err, status)
		return
	}

	// WARNING: don't use s.labels before s.incQueued,
	// since s.labels["cluster_node"] may change inside incQueued.
	if err := s.incQueued(); err != nil {
		limitExcess.With(s.labels).Inc()
		q := getQuerySnippet(req)
		err = fmt.Errorf("%s: %s; query: %q", s, err, q)
		respondWith(rw, err, http.StatusTooManyRequests)
		return
	}
	defer s.dec()

	log.Debugf("%s: request start", s)
	requestSum.With(s.labels).Inc()

	if s.user.allowCORS {
		origin := req.Header.Get("Origin")
		if len(origin) == 0 {
			origin = "*"
		}
		rw.Header().Set("Access-Control-Allow-Origin", origin)
	}

	req.Body = &statReadCloser{
		ReadCloser: req.Body,
		bytesRead:  requestBodyBytes.With(s.labels),
	}
	srw := &statResponseWriter{
		ResponseWriter: rw,
		bytesWritten:   responseBodyBytes.With(s.labels),
	}

	req = s.decorateRequest(req)

	// wrap body into cachedReadCloser, so we could obtain the original
	// request on error.
	req.Body = &cachedReadCloser{
		ReadCloser: req.Body,
	}

	if s.user.cache != nil {
		rp.serveFromCache(s, srw, req)
	} else {
		if !rp.proxyRequest(s, srw, req) {
			// Request timeout.
			srw.statusCode = http.StatusGatewayTimeout
		}
	}

	switch srw.statusCode {
	case http.StatusOK:
		requestSuccess.With(s.labels).Inc()
		log.Debugf("%s: request success", s)
	case http.StatusBadGateway:
		s.host.penalize()
		q := getQuerySnippet(req)
		err := fmt.Errorf("%s: cannot reach %s; query: %q", s, s.host.addr.Host, q)
		respondWith(srw, err, srw.statusCode)
	default:
		log.Debugf("%s: request failure: non-200 status code %d", s, srw.statusCode)
	}

	statusCodes.With(
		prometheus.Labels{
			"user":         s.user.name,
			"cluster":      s.cluster.name,
			"cluster_user": s.clusterUser.name,
			"cluster_node": s.host.addr.Host,
			"code":         strconv.Itoa(srw.statusCode),
		},
	).Inc()
	since := float64(time.Since(timeStart).Seconds())
	requestDuration.With(s.labels).Observe(since)
}

func (rp *reverseProxy) proxyRequest(s *scope, rw http.ResponseWriter, req *http.Request) bool {
	// wrap body into cachedReadCloser, so we could obtain the original
	// request on error.
	if _, ok := req.Body.(*cachedReadCloser); !ok {
		req.Body = &cachedReadCloser{
			ReadCloser: req.Body,
		}
	}

	timeout, timeoutErrMsg := s.getTimeoutWithErrMsg()
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	req = req.WithContext(ctx)
	timeStart := time.Now()

	rp.ReverseProxy.ServeHTTP(rw, req)

	if req.Context().Err() == nil {
		// The request has been successfully proxied.
		since := float64(time.Since(timeStart).Seconds())
		proxiedResponseDuration.With(s.labels).Observe(since)
		return true
	}

	// The request has been timed out.

	// Penalize host with the timed out query, because it may be overloaded.
	s.host.penalize()
	q := getQuerySnippet(req)

	// Forcibly kill the timed out query.
	if err := s.killQuery(); err != nil {
		log.Errorf("%s: cannot kill query: %s; query: %q", s, err, q)
		// Return is skipped intentionally, so the error below
		// may be written to log.
	}

	err := fmt.Errorf("%s: %s; query %q", s, timeoutErrMsg, q)
	respondWith(rw, err, http.StatusGatewayTimeout)
	return false
}

func (rp *reverseProxy) serveFromCache(s *scope, srw *statResponseWriter, req *http.Request) {
	timeStart := time.Now()

	q, err := getFullQuery(req)
	if err != nil {
		err = fmt.Errorf("%s: cannot read query: %s", s, err)
		respondWith(srw, err, http.StatusBadRequest)
		return
	}
	req.Body = ioutil.NopCloser(bytes.NewBuffer(q))

	if !canCacheQuery(q) {
		// The query cannot be cached, so just proxy it.
		if !rp.proxyRequest(s, srw, req) {
			// Request timeout.
			srw.statusCode = http.StatusGatewayTimeout
		}
		return
	}

	// Do not store `cluster_node` in lables, since it has no sense
	// for cache metrics.
	labels := prometheus.Labels{
		"user":         s.labels["user"],
		"cluster":      s.labels["cluster"],
		"cluster_user": s.labels["cluster_user"],
	}

	params := req.URL.Query()
	key := &cache.Key{
		Query:          q,
		AcceptEncoding: req.Header.Get("Accept-Encoding"),
		DefaultFormat:  params.Get("default_format"),
		Database:       params.Get("database"),
	}
	err = s.user.cache.WriteTo(srw, key)
	if err == nil {
		// The response has been successfully served from cache.
		cacheHit.With(labels).Inc()
		since := float64(time.Since(timeStart).Seconds())
		cachedResponseDuration.With(labels).Observe(since)
		log.Debugf("%s: cache hit", s)
		return
	}
	if err != cache.ErrMissing {
		// Unexpected error while serving the response.
		err = fmt.Errorf("%s: %s; query: %q", s, err, q)
		respondWith(srw, err, http.StatusInternalServerError)
		return
	}

	// The response wasn't found in the cache.
	// Request it from clickhouse.
	cacheMiss.With(labels).Inc()
	log.Debugf("%s: cache miss", s)
	crw, err := s.user.cache.NewResponseWriter(srw, key)
	if err != nil {
		err = fmt.Errorf("%s: %s; query: %q", s, err, q)
		respondWith(srw, err, http.StatusInternalServerError)
		return
	}
	if !rp.proxyRequest(s, crw, req) {
		// Request timeout.
		srw.statusCode = http.StatusGatewayTimeout
	}
	if srw.statusCode != http.StatusOK {
		// Do not cache non-200 responses.
		err = crw.Rollback()
	} else {
		err = crw.Commit()
	}

	if err != nil {
		err = fmt.Errorf("%s: %s; query: %q", s, err, q)
		respondWith(srw, err, http.StatusInternalServerError)
		return
	}
}

// ApplyConfig applies provided config to reverseProxy.
// New config will be applied only if non-nil error returned
// Otherwise old version will be kept
func (rp *reverseProxy) ApplyConfig(cfg *config.Config, caches map[string]*cache.Cache) error {
	clusters := make(map[string]*cluster, len(cfg.Clusters))
	for _, c := range cfg.Clusters {
		clusterUsers := make(map[string]*clusterUser, len(c.ClusterUsers))
		for _, cu := range c.ClusterUsers {
			if _, ok := clusterUsers[cu.Name]; ok {
				return fmt.Errorf("cluster user %q already exists", cu.Name)
			}

			var queueCh chan struct{}
			if cu.MaxQueueSize > 0 {
				queueCh = make(chan struct{}, cu.MaxQueueSize)
			}
			clusterUsers[cu.Name] = &clusterUser{
				name:                 cu.Name,
				password:             cu.Password,
				allowedNetworks:      cu.AllowedNetworks,
				reqPerMin:            cu.ReqPerMin,
				maxExecutionTime:     cu.MaxExecutionTime,
				maxConcurrentQueries: cu.MaxConcurrentQueries,
				maxQueueTime:         cu.MaxQueueTime,
				queueCh:              queueCh,
			}
		}

		if _, ok := clusters[c.Name]; ok {
			return fmt.Errorf("cluster %q already exists", c.Name)
		}
		cluster := &cluster{
			name:                  c.Name,
			users:                 clusterUsers,
			heartBeatInterval:     c.HeartBeatInterval,
			killQueryUserName:     c.KillQueryUser.Name,
			killQueryUserPassword: c.KillQueryUser.Password,
		}
		clusters[c.Name] = cluster

		hosts := make([]*host, len(c.Nodes))
		for i, node := range c.Nodes {
			addr, err := url.Parse(fmt.Sprintf("%s://%s", c.Scheme, node))
			if err != nil {
				return err
			}
			hosts[i] = &host{
				cluster: cluster,
				addr:    addr,
			}
		}
		cluster.hosts = hosts
	}

	users := make(map[string]*user, len(cfg.Users))
	for _, u := range cfg.Users {
		c, ok := clusters[u.ToCluster]
		if !ok {
			return fmt.Errorf("error while mapping user %q to cluster %q: no such cluster", u.Name, u.ToCluster)
		}
		if _, ok := c.users[u.ToUser]; !ok {
			return fmt.Errorf("error while mapping user %q to cluster's %q user %q: no such user", u.Name, u.ToCluster, u.ToUser)
		}
		if _, ok := users[u.Name]; ok {
			return fmt.Errorf("user %q already exists", u.Name)
		}
		var queueCh chan struct{}
		if u.MaxQueueSize > 0 {
			queueCh = make(chan struct{}, u.MaxQueueSize)
		}

		var cc *cache.Cache
		if len(u.Cache) > 0 {
			cc = caches[u.Cache]
			if cc == nil {
				return fmt.Errorf("unknown cache %q for user %q ", u.Cache, u.Name)
			}
		}

		users[u.Name] = &user{
			name:                 u.Name,
			password:             u.Password,
			toUser:               u.ToUser,
			cache:                cc,
			denyHTTP:             u.DenyHTTP,
			denyHTTPS:            u.DenyHTTPS,
			allowCORS:            u.AllowCORS,
			toCluster:            u.ToCluster,
			allowedNetworks:      u.AllowedNetworks,
			reqPerMin:            u.ReqPerMin,
			maxExecutionTime:     u.MaxExecutionTime,
			maxConcurrentQueries: u.MaxConcurrentQueries,
			maxQueueTime:         u.MaxQueueTime,
			queueCh:              queueCh,
		}
	}

	// if we are here then there are no errors with new config
	// send signal for all listeners that proxy is going to reload
	close(rp.reloadSignal)
	// wait till all goroutines will stop
	rp.reloadWG.Wait()
	// reset previous hostHealth to remove old hosts
	hostHealth.Reset()
	// recover channel for further reloads
	rp.reloadSignal = make(chan struct{})

	// run checkers
	for _, c := range clusters {
		for _, host := range c.hosts {
			h := host
			rp.reloadWG.Add(1)
			go func() {
				h.runHeartbeat(rp.reloadSignal)
				rp.reloadWG.Done()
			}()
		}
		for _, user := range c.users {
			u := user
			rp.reloadWG.Add(1)
			go func() {
				u.rateLimiter.run(rp.reloadSignal)
				rp.reloadWG.Done()
			}()
		}
	}
	for _, user := range users {
		u := user
		rp.reloadWG.Add(1)
		go func() {
			u.rateLimiter.run(rp.reloadSignal)
			rp.reloadWG.Done()
		}()
	}

	// update configuration
	rp.mu.Lock()
	rp.clusters = clusters
	rp.users = users
	rp.mu.Unlock()

	return nil
}

func (rp *reverseProxy) getScope(req *http.Request) (*scope, int, error) {
	name, password := getAuth(req)

	rp.mu.RLock()
	defer rp.mu.RUnlock()

	u, ok := rp.users[name]
	if !ok {
		return nil, http.StatusUnauthorized, fmt.Errorf("invalid username or password for user %q", name)
	}
	if u.password != password {
		return nil, http.StatusUnauthorized, fmt.Errorf("invalid username or password for user %q", name)
	}
	c, ok := rp.clusters[u.toCluster]
	if !ok {
		panic(fmt.Sprintf("BUG: user %q matches to unknown cluster %q", u.name, u.toCluster))
	}
	cu, ok := c.users[u.toUser]
	if !ok {
		panic(fmt.Sprintf("BUG: user %q matches to unknown user %q at cluster %q", u.name, u.toUser, u.toCluster))
	}
	if u.denyHTTP && req.TLS == nil {
		return nil, http.StatusForbidden, fmt.Errorf("user %q is not allowed to access via http", u.name)
	}
	if u.denyHTTPS && req.TLS != nil {
		return nil, http.StatusForbidden, fmt.Errorf("user %q is not allowed to access via https", u.name)
	}
	if !u.allowedNetworks.Contains(req.RemoteAddr) {
		return nil, http.StatusForbidden, fmt.Errorf("user %q is not allowed to access", u.name)
	}
	if !cu.allowedNetworks.Contains(req.RemoteAddr) {
		return nil, http.StatusForbidden, fmt.Errorf("cluster user %q is not allowed to access", cu.name)
	}
	h := c.getHost()
	if h == nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("cluster %q - no active hosts", u.toCluster)
	}

	var localAddr string
	if addr, ok := req.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		localAddr = addr.String()
	}
	s := &scope{
		id:          atomic.AddUint64(&scopeID, 1),
		host:        h,
		cluster:     c,
		user:        u,
		clusterUser: cu,
		remoteAddr:  req.RemoteAddr,
		localAddr:   localAddr,
		labels: prometheus.Labels{
			"user":         u.name,
			"cluster":      c.name,
			"cluster_user": cu.name,
			"cluster_node": h.addr.Host,
		},
	}
	return s, 0, nil
}
