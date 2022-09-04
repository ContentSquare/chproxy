package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/contentsquare/chproxy/cache"
	"github.com/contentsquare/chproxy/config"
	"github.com/contentsquare/chproxy/log"
	"github.com/prometheus/client_golang/prometheus"
)

// tmpDir temporary path to store ongoing queries results
const tmpDir = "/tmp"

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
	caches   map[string]*cache.AsyncCache
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
	startTime := time.Now()
	s, status, err := rp.getScope(req)
	if err != nil {
		q := getQuerySnippet(req)
		err = fmt.Errorf("%q: %w; query: %q", req.RemoteAddr, err, q)
		respondWith(rw, err, status)
		return
	}

	// WARNING: don't use s.labels before s.incQueued,
	// since `replica` and `cluster_node` may change inside incQueued.
	if err := s.incQueued(); err != nil {
		limitExcess.With(s.labels).Inc()
		q := getQuerySnippet(req)
		err = fmt.Errorf("%s: %w; query: %q", s, err, q)
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

	req, origParams := s.decorateRequest(req)

	// wrap body into cachedReadCloser, so we could obtain the original
	// request on error.
	req.Body = &cachedReadCloser{
		ReadCloser: req.Body,
	}

	// publish session_id if needed
	if s.sessionId != "" {
		rw.Header().Set("X-ClickHouse-Server-Session-Id", s.sessionId)
	}

	if s.user.cache == nil || s.user.cache.Cache == nil {
		rp.proxyRequest(s, srw, srw, req)
	} else {
		rp.serveFromCache(s, srw, req, origParams)
	}

	// It is safe calling getQuerySnippet here, since the request
	// has been already read in proxyRequest or serveFromCache.
	q := getQuerySnippet(req)
	if srw.statusCode == http.StatusOK {
		requestSuccess.With(s.labels).Inc()
		log.Debugf("%s: request success; query: %q; Method: %s; URL: %q", s, q, req.Method, req.URL.String())
	} else {
		log.Debugf("%s: request failure: non-200 status code %d; query: %q; Method: %s; URL: %q", s, srw.statusCode, q, req.Method, req.URL.String())
	}

	statusCodes.With(
		prometheus.Labels{
			"user":         s.user.name,
			"cluster":      s.cluster.name,
			"cluster_user": s.clusterUser.name,
			"replica":      s.host.replica.name,
			"cluster_node": s.host.addr.Host,
			"code":         strconv.Itoa(srw.statusCode),
		},
	).Inc()
	since := time.Since(startTime).Seconds()
	requestDuration.With(s.labels).Observe(since)
}

// proxyRequest proxies the given request to clickhouse and sends response
// to rw.
//
// srw is required only for setting non-200 status codes on timeouts
// or on client connection disconnects.
func (rp *reverseProxy) proxyRequest(s *scope, rw http.ResponseWriter, srw *statResponseWriter, req *http.Request) {
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

	// Cancel the ctx if client closes the remote connection,
	// so the proxied query may be killed instantly.
	ctx, ctxCancel := context.WithCancel(ctx)
	defer ctxCancel()
	// rw must implement http.CloseNotifier.
	ch := rw.(http.CloseNotifier).CloseNotify()
	go func() {
		select {
		case <-ch:
			ctxCancel()
		case <-ctx.Done():
		}
	}()

	req = req.WithContext(ctx)

	startTime := time.Now()
	rp.rp.ServeHTTP(rw, req)

	err := ctx.Err()
	if err == nil { //nolint: gocritic
		// The request has been successfully proxied.
		since := time.Since(startTime).Seconds()
		proxiedResponseDuration.With(s.labels).Observe(since)

		// cache.FSResponseWriter pushes status code to srw on Finalize/Fail actions
		// but they didn't happen yet, so manually propagate the status code from crw to srw.
		if crw, ok := rw.(*cache.TmpFileResponseWriter); ok {
			srw.statusCode = crw.StatusCode()
		}

		// StatusBadGateway response is returned by http.ReverseProxy when
		// it cannot establish connection to remote host.
		if srw.statusCode == http.StatusBadGateway {
			s.host.penalize()
			q := getQuerySnippet(req)
			err := fmt.Errorf("%s: cannot reach %s; query: %q", s, s.host.addr.Host, q)
			respondWith(srw, err, srw.statusCode)
		}
	} else if errors.Is(err, context.Canceled) {
		canceledRequest.With(s.labels).Inc()

		q := getQuerySnippet(req)
		log.Debugf("%s: remote client closed the connection in %s; query: %q", s, time.Since(startTime), q)
		if err := s.killQuery(); err != nil {
			log.Errorf("%s: cannot kill query: %s; query: %q", s, err, q)
		}
		srw.statusCode = 499 // See https://httpstatuses.com/499 .
	} else if errors.Is(err, context.DeadlineExceeded) {
		timeoutRequest.With(s.labels).Inc()

		// Penalize host with the timed out query, because it may be overloaded.
		s.host.penalize()

		q := getQuerySnippet(req)
		log.Debugf("%s: query timeout in %s; query: %q", s, time.Since(startTime), q)
		if err := s.killQuery(); err != nil {
			log.Errorf("%s: cannot kill query: %s; query: %q", s, err, q)
		}
		err = fmt.Errorf("%s: %w; query: %q", s, timeoutErrMsg, q)
		respondWith(rw, err, http.StatusGatewayTimeout)
		srw.statusCode = http.StatusGatewayTimeout
	} else {
		panic(fmt.Sprintf("BUG: context.Context.Err() returned unexpected error: %s", err))
	}
}

func (rp *reverseProxy) serveFromCache(s *scope, srw *statResponseWriter, req *http.Request, origParams url.Values) {
	noCache := origParams.Get("no_cache")
	if noCache == "1" || noCache == "true" {
		// The response caching is disabled.
		rp.proxyRequest(s, srw, srw, req)
		return
	}

	q, err := getFullQuery(req)
	if err != nil {
		err = fmt.Errorf("%s: cannot read query: %w", s, err)
		respondWith(srw, err, http.StatusBadRequest)
		return
	}
	if !canCacheQuery(q) {
		// The query cannot be cached, so just proxy it.
		rp.proxyRequest(s, srw, srw, req)
		return
	}

	// Do not store `replica` and `cluster_node` in labels, since they have
	// no sense for cache metrics.
	labels := prometheus.Labels{
		"cache":        s.user.cache.Name(),
		"user":         s.labels["user"],
		"cluster":      s.labels["cluster"],
		"cluster_user": s.labels["cluster_user"],
	}

	var userParamsHash uint32
	if s.user.params != nil {
		userParamsHash = s.user.params.key
	}

	queryParamsHash := calcQueryParamsHash(origParams)

	key := cache.NewKey(skipLeadingComments(q), origParams, sortHeader(req.Header.Get("Accept-Encoding")), userParamsHash, queryParamsHash)

	startTime := time.Now()
	userCache := s.user.cache
	// Try to serve from cache
	cachedData, err := userCache.Get(key)
	if err == nil {
		// The response has been successfully served from cache.
		defer cachedData.Data.Close()
		cacheHit.With(labels).Inc()
		since := time.Since(startTime).Seconds()
		cachedResponseDuration.With(labels).Observe(since)
		log.Debugf("%s: cache hit", s)
		_ = RespondWithData(srw, cachedData.Data, cachedData.ContentMetadata, cachedData.Ttl, http.StatusOK)
		return
	}
	// Await for potential result from concurrent query
	transactionState, err := userCache.AwaitForConcurrentTransaction(key)
	if err != nil {
		// log and continue processing
		log.Errorf("failed to await for concurrent transaction due to: %v", err)
	} else {
		if transactionState.IsCompleted() {
			cachedData, err := userCache.Get(key)
			if err == nil {
				defer cachedData.Data.Close()
				_ = RespondWithData(srw, cachedData.Data, cachedData.ContentMetadata, cachedData.Ttl, http.StatusOK)
				cacheHitFromConcurrentQueries.With(labels).Inc()
				log.Debugf("%s: cache hit after awaiting concurrent query", s)
				return
			} else {
				cacheMissFromConcurrentQueries.With(labels).Inc()
				log.Debugf("%s: cache miss after awaiting concurrent query", s)
			}
		} else if transactionState.IsFailed() {
			err = fmt.Errorf("%s: concurrent query failed", s)
			respondWith(srw, err, http.StatusInternalServerError)
			return
		}
	}

	// The response wasn't found in the cache.
	// Request it from clickhouse.
	tmpFileRespWriter, err := cache.NewTmpFileResponseWriter(srw, tmpDir)
	if err != nil {
		err = fmt.Errorf("%s: %w; query: %q", s, err, q)
		respondWith(srw, err, http.StatusInternalServerError)
		return
	}
	defer tmpFileRespWriter.Close()

	// Initialise transaction
	err = userCache.Create(key)
	if err != nil {
		log.Errorf("%s: %s; query: %q - failed to register transaction", s, err, q)
	}

	// proxy request and capture response along with headers to [[TmpFileResponseWriter]]
	rp.proxyRequest(s, tmpFileRespWriter, srw, req)

	contentEncoding := tmpFileRespWriter.GetCapturedContentEncoding()
	contentType := tmpFileRespWriter.GetCapturedContentType()
	contentLength, err := tmpFileRespWriter.GetCapturedContentLength()
	if err != nil {
		log.Errorf("%s: %s; query: %q - failed to get contentLength of query", s, err, q)
		respondWith(srw, err, http.StatusInternalServerError)
		return
	}
	reader, err := tmpFileRespWriter.Reader()
	if err != nil {
		log.Errorf("%s: %s; query: %q - failed to get Reader from tmp file", s, err, q)
		respondWith(srw, err, http.StatusInternalServerError)
		return
	}
	contentMetadata := cache.ContentMetadata{Length: contentLength, Encoding: contentEncoding, Type: contentType}

	statusCode := tmpFileRespWriter.StatusCode()
	if statusCode != http.StatusOK || s.canceled {
		// Do not cache non-200 or cancelled responses.
		// Restore the original status code by proxyRequest if it was set.
		if srw.statusCode != 0 {
			tmpFileRespWriter.WriteHeader(srw.statusCode)
		}
		rp.completeTransaction(s, statusCode, userCache, key, q)
		err = RespondWithData(srw, reader, contentMetadata, 0*time.Second, statusCode)
		if err != nil {
			err = fmt.Errorf("%s: %w; query: %q", s, err, q)
			respondWith(srw, err, http.StatusInternalServerError)
			return
		}
	} else {
		cacheMiss.With(labels).Inc()
		log.Debugf("%s: cache miss", s)
		expiration, err := userCache.Put(reader, contentMetadata, key)
		if err != nil {
			log.Errorf("%s: %s; query: %q - failed to put response in the cache", s, err, q)
		}
		rp.completeTransaction(s, statusCode, userCache, key, q)

		// we need to reset the offset since the reader of tmpFileRespWriter was already
		// consumed in RespondWithData(...)
		err = tmpFileRespWriter.ResetFileOffset()
		if err != nil {
			err = fmt.Errorf("%s: %w; query: %q", s, err, q)
			respondWith(srw, err, http.StatusInternalServerError)
			return
		}
		err = RespondWithData(srw, reader, contentMetadata, expiration, statusCode)
		if err != nil {
			err = fmt.Errorf("%s: %w; query: %q", s, err, q)
			respondWith(srw, err, http.StatusInternalServerError)
			return
		}
	}
}

// clickhouseRecoverableStatusCodes set of recoverable http responses' status codes from Clickhouse.
// When such happens we mark transaction as completed and let concurrent query to hit another Clickhouse shard.
var clickhouseRecoverableStatusCodes = map[int]struct{}{503: {}}

func (rp *reverseProxy) completeTransaction(s *scope, statusCode int, userCache *cache.AsyncCache, key *cache.Key, q []byte) {
	if statusCode < 300 {
		if err := userCache.Complete(key); err != nil {
			log.Errorf("%s: %s; query: %q", s, err, q)
		}
		return
	}

	if _, ok := clickhouseRecoverableStatusCodes[statusCode]; ok {
		if err := userCache.Complete(key); err != nil {
			log.Errorf("%s: %s; query: %q", s, err, q)
		}
	} else {
		if err := userCache.Fail(key); err != nil {
			log.Errorf("%s: %s; query: %q", s, err, q)
		}
	}
}

func calcQueryParamsHash(origParams url.Values) uint32 {
	queryParams := make(map[string]string)
	for param := range origParams {
		if strings.HasPrefix(param, "param_") {
			queryParams[param] = origParams.Get(param)
		}
	}
	var queryParamsHash, err = calcMapHash(queryParams)
	if err != nil {
		log.Errorf("fail to calc hash for params %s; %s", origParams, err)
		return 0
	}
	return queryParamsHash
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

	caches := make(map[string]*cache.AsyncCache, len(cfg.Caches))
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

	// transactionsTimeout used for creation of transactions registry inside async cache.
	// It is set to the highest configured execution time of all users to avoid setups were users use the same cache and have configured different maxExecutionTime.
	// This would provoke undesired behaviour of `dogpile effect`
	transactionsTimeout := config.Duration(0)
	for _, user := range cfg.Users {
		if user.MaxExecutionTime > transactionsTimeout {
			transactionsTimeout = user.MaxExecutionTime
		}
	}

	for _, cc := range cfg.Caches {
		if _, ok := caches[cc.Name]; ok {
			return fmt.Errorf("duplicate config for cache %q", cc.Name)
		}

		tmpCache, err := cache.NewAsyncCache(cc, time.Duration(transactionsTimeout))
		if err != nil {
			return err
		}
		caches[cc.Name] = tmpCache
	}

	params := make(map[string]*paramsRegistry, len(cfg.ParamGroups))
	for _, p := range cfg.ParamGroups {
		if _, ok := params[p.Name]; ok {
			return fmt.Errorf("duplicate config for ParamGroups %q", p.Name)
		}
		params[p.Name], err = newParamsRegistry(p.Params)
		if err != nil {
			return fmt.Errorf("cannot initialize params %q: %w", p.Name, err)
		}
	}

	profile := &usersProfile{
		cfg:      cfg.Users,
		clusters: clusters,
		caches:   caches,
		params:   params,
	}
	users, err := profile.newUsers()
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
		for _, r := range c.replicas {
			for _, h := range r.hosts {
				rp.reloadWG.Add(1)
				go func(h *host) {
					h.runHeartbeat(rp.reloadSignal)
					rp.reloadWG.Done()
				}(h)
			}
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

// refreshCacheMetrics refreshes cacheSize and cacheItems metrics.
func (rp *reverseProxy) refreshCacheMetrics() {
	rp.lock.RLock()
	defer rp.lock.RUnlock()

	for _, c := range rp.caches {
		stats := c.Stats()
		labels := prometheus.Labels{
			"cache": c.Name(),
		}
		cacheSize.With(labels).Set(float64(stats.Size))
		cacheItems.With(labels).Set(float64(stats.Items))
	}
}

func (rp *reverseProxy) getScope(req *http.Request) (*scope, int, error) {
	name, password := getAuth(req)
	sessionId := getSessionId(req)
	sessionTimeout := getSessionTimeout(req)
	var (
		u  *user
		c  *cluster
		cu *clusterUser
	)

	rp.lock.RLock()
	u = rp.users[name]
	if u != nil {
		// c and cu for toCluster and toUser must exist if applyConfig
		// is correct.
		// Fix applyConfig if c or cu equal to nil.
		c = rp.clusters[u.toCluster]
		cu = c.users[u.toUser]
	}
	rp.lock.RUnlock()

	if u == nil {
		return nil, http.StatusUnauthorized, fmt.Errorf("invalid username or password for user %q", name)
	}
	if u.password != password {
		return nil, http.StatusUnauthorized, fmt.Errorf("invalid username or password for user %q", name)
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

	s := newScope(req, u, c, cu, sessionId, sessionTimeout)
	return s, 0, nil
}
