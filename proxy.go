package main

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"sync"
	"time"

	"github.com/Vertamedia/chproxy/cache"
	"github.com/Vertamedia/chproxy/config"
	"github.com/Vertamedia/chproxy/log"
	"github.com/prometheus/client_golang/prometheus"
)

type reverseProxy struct {
	rp *httputil.ReverseProxy

	// configLock serializes access to applyConfig.
	// It protects reload* fields.
	configLock sync.Mutex

	reloadSignal chan struct{}
	reloadWG     sync.WaitGroup

	// lock protects users, clusters and caches.
	// RWMutex enables concurrent access to getScope.
	lock sync.RWMutex

	users    map[string]*user
	clusters map[string]*cluster
	caches   map[string]*cache.Cache
}

func newReverseProxy() *reverseProxy {
	return &reverseProxy{
		rp: &httputil.ReverseProxy{
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

	nc := req.URL.Query().Get("no_cache")
	noCache := (nc == "1" || nc == "true")

	req = s.decorateRequest(req)

	// wrap body into cachedReadCloser, so we could obtain the original
	// request on error.
	req.Body = &cachedReadCloser{
		ReadCloser: req.Body,
	}

	if s.user.cache == nil || noCache {
		if !rp.proxyRequest(s, srw, req) {
			// Request timeout.
			srw.statusCode = http.StatusGatewayTimeout
		}
	} else {
		rp.serveFromCache(s, srw, req)
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
	rp.rp.ServeHTTP(rw, req)

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
		"cache":        s.user.cache.Name,
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

	timeStart := time.Now()
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
	if crw.StatusCode() != http.StatusOK {
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

// applyConfig applies the given cfg to reverseProxy.
//
// New config is applied only if non-nil error returned.
// Otherwise old config version is kept.
func (rp *reverseProxy) applyConfig(cfg *config.Config) error {
	// configLock protects from concurrent calls to applyConfig
	// by serializing such calls.
	// configLock shouldn't be used in other places.
	rp.configLock.Lock()
	defer rp.configLock.Unlock()

	clusters, err := newClusters(cfg.Clusters)
	if err != nil {
		return err
	}

	caches := make(map[string]*cache.Cache, len(cfg.Caches))
	defer func() {
		// caches is swapped with old caches from rp.caches
		// on successful config reload - see the end of reloadConfig.
		for _, tmpCache := range caches {
			// Speed up applyConfig by closing caches in background,
			// since the process of cache closing may be lengthy
			// due to cleaning.
			go tmpCache.Close()
		}
	}()
	for _, cc := range cfg.Caches {
		if _, ok := caches[cc.Name]; ok {
			return fmt.Errorf("duplicate config for cache %q", cc.Name)
		}
		tmpCache, err := cache.New(cc)
		if err != nil {
			return fmt.Errorf("cannot initialize cache %q: %s", cc.Name, err)
		}
		caches[cc.Name] = tmpCache
	}

	users, err := newUsers(cfg.Users, clusters, caches)
	if err != nil {
		return err
	}

	// New configs have been successfully prepared.
	// Restart service goroutines with new configs.

	// Stop the previous service goroutines.
	close(rp.reloadSignal)
	rp.reloadWG.Wait()
	rp.reloadSignal = make(chan struct{})

	// Reset metrics from the previous configs, which may become irrelevant
	// with new configs.
	// Counters and Summary metrics are always relevant.
	// Gauge metrics may become irrelevant if they may freeze at non-zero
	// value after config reload.
	hostHealth.Reset()
	cacheSize.Reset()
	cacheItems.Reset()

	// Start service goroutines with new configs.
	for _, c := range clusters {
		for _, h := range c.hosts {
			rp.reloadWG.Add(1)
			go func(h *host) {
				h.runHeartbeat(rp.reloadSignal)
				rp.reloadWG.Done()
			}(h)
		}
		for _, cu := range c.users {
			rp.reloadWG.Add(1)
			go func(cu *clusterUser) {
				cu.rateLimiter.run(rp.reloadSignal)
				rp.reloadWG.Done()
			}(cu)
		}
	}
	for _, u := range users {
		rp.reloadWG.Add(1)
		go func(u *user) {
			u.rateLimiter.run(rp.reloadSignal)
			rp.reloadWG.Done()
		}(u)
	}

	// Substitute old configs with the new configs in rp.
	// All the currently running requests will continue with old configs,
	// while all the new requests will use new configs.
	rp.lock.Lock()
	rp.clusters = clusters
	rp.users = users
	// Swap is needed for deferred closing of old caches.
	// See the code above where new caches are created.
	caches, rp.caches = rp.caches, caches
	rp.lock.Unlock()

	return nil
}

// refreshCacheMetrics refresehs cacheSize and cacheItems metrics.
func (rp *reverseProxy) refreshCacheMetrics() {
	rp.lock.RLock()
	defer rp.lock.RUnlock()

	for _, c := range rp.caches {
		stats := c.Stats()
		labels := prometheus.Labels{
			"cache": c.Name,
		}
		cacheSize.With(labels).Set(float64(stats.Size))
		cacheItems.With(labels).Set(float64(stats.Items))
	}
}

func (rp *reverseProxy) getScope(req *http.Request) (*scope, int, error) {
	name, password := getAuth(req)

	rp.lock.RLock()
	defer rp.lock.RUnlock()

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
		id:          newScopeID(),
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
