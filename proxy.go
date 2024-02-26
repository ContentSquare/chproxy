package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/contentsquare/chproxy/cache"
	"github.com/contentsquare/chproxy/config"
	"github.com/contentsquare/chproxy/internal/topology"
	"github.com/contentsquare/chproxy/log"
	"github.com/prometheus/client_golang/prometheus"
)

// tmpDir temporary path to store ongoing queries results
const tmpDir = "/tmp"

// failedTransactionPrefix prefix added to the failed reason for concurrent queries registry
const failedTransactionPrefix = "[concurrent query failed]"

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

	users               map[string]*user
	clusters            map[string]*cluster
	caches              map[string]*cache.AsyncCache
	hasWildcarded       bool
	maxIdleConns        int
	maxIdleConnsPerHost int
	maxErrorReasonSize  int64
}

func newReverseProxy(cfgCp *config.ConnectionPool) *reverseProxy {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}
			return dialer.DialContext(ctx, network, addr)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          cfgCp.MaxIdleConns,
		MaxIdleConnsPerHost:   cfgCp.MaxIdleConnsPerHost,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &reverseProxy{
		rp: &httputil.ReverseProxy{
			Director:  func(*http.Request) {},
			Transport: transport,

			// Suppress error logging in ReverseProxy, since all the errors
			// are handled and logged in the code below.
			ErrorLog: log.NilLogger,
		},
		reloadSignal:        make(chan struct{}),
		reloadWG:            sync.WaitGroup{},
		maxIdleConns:        cfgCp.MaxIdleConnsPerHost,
		maxIdleConnsPerHost: cfgCp.MaxIdleConnsPerHost,
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

	q, shouldReturnFromCache, err := shouldRespondFromCache(s, origParams, req)
	if err != nil {
		respondWith(srw, err, http.StatusBadRequest)
		return
	}

	if shouldReturnFromCache {
		rp.serveFromCache(s, srw, req, origParams, q)
	} else {
		rp.proxyRequest(s, srw, srw, req)
	}

	// It is safe calling getQuerySnippet here, since the request
	// has been already read in proxyRequest or serveFromCache.
	query := getQuerySnippet(req)
	if srw.statusCode == http.StatusOK {
		requestSuccess.With(s.labels).Inc()
		log.Debugf("%s: request success; query: %q; Method: %s; URL: %q", s, query, req.Method, req.URL.String())
	} else {
		log.Debugf("%s: request failure: non-200 status code %d; query: %q; Method: %s; URL: %q", s, srw.statusCode, query, req.Method, req.URL.String())
	}

	statusCodes.With(
		prometheus.Labels{
			"user":         s.user.name,
			"cluster":      s.cluster.name,
			"cluster_user": s.clusterUser.name,
			"replica":      s.host.ReplicaName(),
			"cluster_node": s.host.Host(),
			"code":         strconv.Itoa(srw.statusCode),
		},
	).Inc()
	since := time.Since(startTime).Seconds()
	requestDuration.With(s.labels).Observe(since)
}

func shouldRespondFromCache(s *scope, origParams url.Values, req *http.Request) ([]byte, bool, error) {
	if s.user.cache == nil || s.user.cache.Cache == nil {
		return nil, false, nil
	}

	noCache := origParams.Get("no_cache")
	if noCache == "1" || noCache == "true" {
		return nil, false, nil
	}

	q, err := getFullQuery(req)
	if err != nil {
		return nil, false, fmt.Errorf("%s: cannot read query: %w", s, err)
	}

	return q, canCacheQuery(q), nil
}

func executeWithRetry(
	ctx context.Context,
	s *scope,
	maxRetry int,
	rp func(http.ResponseWriter, *http.Request),
	rw ResponseWriterWithCode,
	srw StatResponseWriter,
	req *http.Request,
	monitorDuration func(float64),
	monitorRetryRequestInc func(prometheus.Labels),
) (float64, error) {
	startTime := time.Now()
	var since float64

	// Use readAndRestoreRequestBody to read the entire request body into a byte slice,
	// and to restore req.Body so that it can be reused later in the code.
	body, err := readAndRestoreRequestBody(req)
	if err != nil {
		since := time.Since(startTime).Seconds()
		return since, err
	}

	numRetry := 0
	for {
		rp(rw, req)

		// Restore req.Body after it's consumed by 'rp' for potential reuse.
		req.Body = io.NopCloser(bytes.NewBuffer(body))

		err := ctx.Err()
		if err != nil {
			since = time.Since(startTime).Seconds()

			return since, err
		}
		// The request has been successfully proxied.

		srw.SetStatusCode(rw.StatusCode())
		// StatusBadGateway response is returned by http.ReverseProxy when
		// it cannot establish connection to remote host.
		if rw.StatusCode() == http.StatusBadGateway {
			log.Debugf("the invalid host is: %s", s.host)
			s.host.Penalize()
			// comment s.host.dec() line to avoid double increment; issue #322
			// s.host.dec()
			s.host.SetIsActive(false)
			nextHost := s.cluster.getHost()
			// The query could be retried if it has no stickiness to a certain server
			if numRetry < maxRetry && nextHost.IsActive() && s.sessionId == "" {
				// the query execution has been failed
				monitorRetryRequestInc(s.labels)
				currentHost := s.host

				// decrement the current failed host counter and increment the new host
				// as for the end of the requests we will close the scope and in that closed scope
				// decrement the new host PR - https://github.com/ContentSquare/chproxy/pull/357
				if currentHost != nextHost {
					currentHost.DecrementConnections()
					nextHost.IncrementConnections()
				}
				// update host
				s.host = nextHost

				req.URL.Host = s.host.Host()
				req.URL.Scheme = s.host.Scheme()
				log.Debugf("the valid host is: %s", s.host)
			} else {
				since = time.Since(startTime).Seconds()
				monitorDuration(since)
				q := getQuerySnippet(req)
				err1 := fmt.Errorf("%s: cannot reach %s; query: %q", s, s.host.Host(), q)
				respondWith(srw, err1, srw.StatusCode())
				break
			}
		} else {
			since = time.Since(startTime).Seconds()
			break
		}
		numRetry++
	}
	return since, nil
}

// proxyRequest proxies the given request to clickhouse and sends response
// to rw.
//
// srw is required only for setting non-200 status codes on timeouts
// or on client connection disconnects.
func (rp *reverseProxy) proxyRequest(s *scope, rw ResponseWriterWithCode, srw *statResponseWriter, req *http.Request) {
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
	ctx, ctxCancel := listenToCloseNotify(ctx, rw)
	defer ctxCancel()

	req = req.WithContext(ctx)

	startTime := time.Now()

	executeDuration, err := executeWithRetry(ctx, s, s.cluster.retryNumber, rp.rp.ServeHTTP, rw, srw, req, func(duration float64) {
		proxiedResponseDuration.With(s.labels).Observe(duration)
	}, func(labels prometheus.Labels) { retryRequest.With(labels).Inc() })

	switch {
	case err == nil:
		return
	case errors.Is(err, context.Canceled):
		canceledRequest.With(s.labels).Inc()

		q := getQuerySnippet(req)
		log.Debugf("%s: remote client closed the connection in %s; query: %q", s, time.Since(startTime), q)
		if err := s.killQuery(); err != nil {
			log.Errorf("%s: cannot kill query: %s; query: %q", s, err, q)
		}
		srw.statusCode = 499 // See https://httpstatuses.com/499 .
	case errors.Is(err, context.DeadlineExceeded):
		timeoutRequest.With(s.labels).Inc()

		// Penalize host with the timed out query, because it may be overloaded.
		s.host.Penalize()

		q := getQuerySnippet(req)
		log.Debugf("%s: query timeout in %f; query: %q", s, executeDuration, q)
		if err := s.killQuery(); err != nil {
			log.Errorf("%s: cannot kill query: %s; query: %q", s, err, q)
		}
		err = fmt.Errorf("%s: %w; query: %q", s, timeoutErrMsg, q)
		respondWith(rw, err, http.StatusGatewayTimeout)
		srw.statusCode = http.StatusGatewayTimeout
	default:
		panic(fmt.Sprintf("BUG: context.Context.Err() returned unexpected error: %s", err))
	}
}

func listenToCloseNotify(ctx context.Context, rw ResponseWriterWithCode) (context.Context, context.CancelFunc) {
	// Cancel the ctx if client closes the remote connection,
	// so the proxied query may be killed instantly.
	ctx, ctxCancel := context.WithCancel(ctx)

	// rw must implement http.CloseNotifier.
	rwc, ok := rw.(http.CloseNotifier)
	if !ok {
		panic("BUG: the wrapped ResponseWriter must implement http.CloseNotifier")
	}

	ch := rwc.CloseNotify()
	go func() {
		select {
		case <-ch:
			ctxCancel()
		case <-ctx.Done():
		}
	}()

	return ctx, ctxCancel
}

//nolint:cyclop //TODO refactor this method, most likely requires some work.
func (rp *reverseProxy) serveFromCache(s *scope, srw *statResponseWriter, req *http.Request, origParams url.Values, q []byte) {
	labels := makeCacheLabels(s)
	key := newCacheKey(s, origParams, q, req)

	startTime := time.Now()
	userCache := s.user.cache
	// Try to serve from cache
	cachedData, err := userCache.Get(key)
	if err == nil {
		// The response has been successfully served from cache.
		defer cachedData.Data.Close()
		cacheHit.With(labels).Inc()
		cachedResponseDuration.With(labels).Observe(time.Since(startTime).Seconds())
		log.Debugf("%s: cache hit", s)
		_ = RespondWithData(srw, cachedData.Data, cachedData.ContentMetadata, cachedData.Ttl, XCacheHit, http.StatusOK, labels)
		return
	}
	// Await for potential result from concurrent query
	transactionStatus, err := userCache.AwaitForConcurrentTransaction(key)
	if err != nil {
		// log and continue processing
		log.Errorf("failed to await for concurrent transaction due to: %v", err)
	} else {
		if transactionStatus.State.IsCompleted() {
			cachedData, err := userCache.Get(key)
			if err == nil {
				defer cachedData.Data.Close()
				_ = RespondWithData(srw, cachedData.Data, cachedData.ContentMetadata, cachedData.Ttl, XCacheHit, http.StatusOK, labels)
				cacheHitFromConcurrentQueries.With(labels).Inc()
				log.Debugf("%s: cache hit after awaiting concurrent query", s)
				return
			} else {
				cacheMissFromConcurrentQueries.With(labels).Inc()
				log.Debugf("%s: cache miss after awaiting concurrent query", s)
			}
		} else if transactionStatus.State.IsFailed() {
			respondWith(srw, fmt.Errorf(transactionStatus.FailReason), http.StatusInternalServerError)
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

		errReason := "unknown error reason"
		if contentLength > rp.maxErrorReasonSize {
			log.Infof("%s: Error reason length (%d) is greater than max error reason size (%d)", s, contentLength, rp.maxErrorReasonSize)
		} else {
			errString, err := toString(reader)
			if err != nil {
				log.Errorf("%s failed to get error reason: %s", s, err.Error())
			}

			errReason = fmt.Sprintf("%s %s", failedTransactionPrefix, errString)
		}

		rp.completeTransaction(s, statusCode, userCache, key, q, errReason)

		// we need to reset the offset since the reader of tmpFileRespWriter was already
		// consumed in RespondWithData(...)
		err = tmpFileRespWriter.ResetFileOffset()
		if err != nil {
			err = fmt.Errorf("%s: %w; query: %q", s, err, q)
			respondWith(srw, err, http.StatusInternalServerError)
			return
		}

		err = RespondWithData(srw, reader, contentMetadata, 0*time.Second, XCacheMiss, statusCode, labels)
		if err != nil {
			err = fmt.Errorf("%s: %w; query: %q", s, err, q)
			respondWith(srw, err, http.StatusInternalServerError)
		}
	} else {
		// Do not cache responses greater than max payload size.
		if contentLength > int64(s.user.cache.MaxPayloadSize) {
			cacheSkipped.With(labels).Inc()
			log.Infof("%s: Request will not be cached. Content length (%d) is greater than max payload size (%d)", s, contentLength, s.user.cache.MaxPayloadSize)

			rp.completeTransaction(s, statusCode, userCache, key, q, "")

			err = RespondWithData(srw, reader, contentMetadata, 0*time.Second, XCacheNA, tmpFileRespWriter.StatusCode(), labels)
			if err != nil {
				err = fmt.Errorf("%s: %w; query: %q", s, err, q)
				respondWith(srw, err, http.StatusInternalServerError)
			}
			return
		}
		cacheMiss.With(labels).Inc()
		log.Debugf("%s: cache miss", s)
		expiration, err := userCache.Put(reader, contentMetadata, key)
		if err != nil {
			cacheFailedInsert.With(labels).Inc()
			log.Errorf("%s: %s; query: %q - failed to put response in the cache", s, err, q)
		}
		rp.completeTransaction(s, statusCode, userCache, key, q, "")

		// we need to reset the offset since the reader of tmpFileRespWriter was already
		// consumed in RespondWithData(...)
		err = tmpFileRespWriter.ResetFileOffset()
		if err != nil {
			err = fmt.Errorf("%s: %w; query: %q", s, err, q)
			respondWith(srw, err, http.StatusInternalServerError)
			return
		}
		err = RespondWithData(srw, reader, contentMetadata, expiration, XCacheMiss, statusCode, labels)
		if err != nil {
			err = fmt.Errorf("%s: %w; query: %q", s, err, q)
			respondWith(srw, err, http.StatusInternalServerError)
			return
		}
	}
}

func makeCacheLabels(s *scope) prometheus.Labels {
	// Do not store `replica` and `cluster_node` in labels, since they have
	// no sense for cache metrics.
	return prometheus.Labels{
		"cache":        s.user.cache.Name(),
		"user":         s.labels["user"],
		"cluster":      s.labels["cluster"],
		"cluster_user": s.labels["cluster_user"],
	}
}

func newCacheKey(s *scope, origParams url.Values, q []byte, req *http.Request) *cache.Key {
	var userParamsHash uint32
	if s.user.params != nil {
		userParamsHash = s.user.params.key
	}

	queryParamsHash := calcQueryParamsHash(origParams)
	credHash, err := uint32(0), error(nil)

	if !s.user.cache.SharedWithAllUsers {
		credHash, err = calcCredentialHash(s.clusterUser.name, s.clusterUser.password)
	}
	if err != nil {
		log.Errorf("fail to calc hash on credentials for user %s", s.user.name)
		credHash = 0
	}

	return cache.NewKey(
		skipLeadingComments(q),
		origParams,
		sortHeader(req.Header.Get("Accept-Encoding")),
		userParamsHash,
		queryParamsHash,
		credHash,
	)
}

func toString(stream io.Reader) (string, error) {
	buf := new(bytes.Buffer)

	_, err := buf.ReadFrom(stream)
	if err != nil {
		return "", err
	}

	return bytes.NewBuffer(buf.Bytes()).String(), nil
}

// clickhouseRecoverableStatusCodes set of recoverable http responses' status codes from Clickhouse.
// When such happens we mark transaction as completed and let concurrent query to hit another Clickhouse shard.
// possible http error codes in clickhouse (i.e: https://github.com/ClickHouse/ClickHouse/blob/master/src/Server/HTTPHandler.cpp)
var clickhouseRecoverableStatusCodes = map[int]struct{}{http.StatusServiceUnavailable: {}, http.StatusRequestTimeout: {}}

func (rp *reverseProxy) completeTransaction(s *scope, statusCode int, userCache *cache.AsyncCache, key *cache.Key,
	q []byte,
	failReason string,
) {
	// complete successful transactions or those with empty fail reason
	if statusCode < 300 || failReason == "" {
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
		if err := userCache.Fail(key, failReason); err != nil {
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
	queryParamsHash, err := calcMapHash(queryParams)
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

	rp.maxErrorReasonSize = int64(cfg.MaxErrorReasonSize)

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
		if user.IsWildcarded {
			rp.hasWildcarded = true
		}
	}

	if err := initTempCaches(caches, transactionsTimeout, cfg.Caches); err != nil {
		return err
	}

	params, err := paramsFromConfig(cfg.ParamGroups)
	if err != nil {
		return err
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

	if err := validateNoWildcardedUserForHeartbeat(clusters, cfg.Clusters); err != nil {
		return err
	}

	// New configs have been successfully prepared.
	// Restart service goroutines with new configs.

	// Stop the previous service goroutines.
	close(rp.reloadSignal)
	rp.reloadWG.Wait()
	rp.reloadSignal = make(chan struct{})
	rp.restartWithNewConfig(caches, clusters, users)

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

func initTempCaches(caches map[string]*cache.AsyncCache, transactionsTimeout config.Duration, cfg []config.Cache) error {
	for _, cc := range cfg {
		if _, ok := caches[cc.Name]; ok {
			return fmt.Errorf("duplicate config for cache %q", cc.Name)
		}

		tmpCache, err := cache.NewAsyncCache(cc, time.Duration(transactionsTimeout))
		if err != nil {
			return err
		}
		caches[cc.Name] = tmpCache
	}

	return nil
}

func paramsFromConfig(cfg []config.ParamGroup) (map[string]*paramsRegistry, error) {
	params := make(map[string]*paramsRegistry, len(cfg))
	for _, p := range cfg {
		if _, ok := params[p.Name]; ok {
			return nil, fmt.Errorf("duplicate config for ParamGroups %q", p.Name)
		}
		registry, err := newParamsRegistry(p.Params)
		if err != nil {
			return nil, fmt.Errorf("cannot initialize params %q: %w", p.Name, err)
		}

		params[p.Name] = registry
	}

	return params, nil
}

func validateNoWildcardedUserForHeartbeat(clusters map[string]*cluster, cfg []config.Cluster) error {
	for c := range cfg {
		cfgcl := cfg[c]
		clname := cfgcl.Name
		cuname := cfgcl.ClusterUsers[0].Name
		heartbeat := cfg[c].HeartBeat
		cl := clusters[clname]
		cu := cl.users[cuname]

		if cu.isWildcarded {
			if heartbeat.Request != "/ping" && len(heartbeat.User) == 0 {
				return fmt.Errorf(
					"`cluster.heartbeat.user ` cannot be unset for %q because a wildcarded user cannot send heartbeat",
					clname,
				)
			}
		}
	}

	return nil
}

func (rp *reverseProxy) restartWithNewConfig(caches map[string]*cache.AsyncCache, clusters map[string]*cluster, users map[string]*user) {
	// Reset metrics from the previous configs, which may become irrelevant
	// with new configs.
	// Counters and Summary metrics are always relevant.
	// Gauge metrics may become irrelevant if they may freeze at non-zero
	// value after config reload.
	topology.HostHealth.Reset()
	cacheSize.Reset()
	cacheItems.Reset()

	// Start service goroutines with new configs.
	for _, c := range clusters {
		for _, r := range c.replicas {
			for _, h := range r.hosts {
				rp.reloadWG.Add(1)
				go func(h *topology.Node) {
					h.StartHeartbeat(rp.reloadSignal)
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

// find user, cluster and clusterUser
// in case of wildcarded user, cluster user is crafted to use original credentials
func (rp *reverseProxy) getUser(name string, password string) (found bool, u *user, c *cluster, cu *clusterUser) {
	rp.lock.RLock()
	defer rp.lock.RUnlock()
	found = false
	u = rp.users[name]
	switch {
	case u != nil:
		found = (u.password == password)
		// existence of c and cu for toCluster is guaranteed by applyConfig
		c = rp.clusters[u.toCluster]
		cu = c.users[u.toUser]
	case name == "" || name == defaultUser:
		// default user can't work with the wildcarded feature for security reasons
		found = false
	case rp.hasWildcarded:
		// checking if we have wildcarded users and if username matches one 3 possibles patterns
		found, u, c, cu = rp.findWildcardedUserInformation(name, password)
	}
	return found, u, c, cu
}

func (rp *reverseProxy) findWildcardedUserInformation(name string, password string) (found bool, u *user, c *cluster, cu *clusterUser) {
	// cf a validation in config.go, the names must contains either a prefix, a suffix or a wildcard
	// the wildcarded user is "*"
	// the wildcarded user is "*[suffix]"
	// the wildcarded user is "[prefix]*"
	for _, user := range rp.users {
		if user.isWildcarded {
			s := strings.Split(user.name, "*")
			switch {
			case s[0] == "" && s[1] == "":
				return rp.generateWildcardedUserInformation(user, name, password)
			case s[0] == "":
				suffix := s[1]
				if strings.HasSuffix(name, suffix) {
					return rp.generateWildcardedUserInformation(user, name, password)
				}
			case s[1] == "":
				prefix := s[0]
				if strings.HasPrefix(name, prefix) {
					return rp.generateWildcardedUserInformation(user, name, password)
				}
			}
		}
	}
	return false, nil, nil, nil
}

func (rp *reverseProxy) generateWildcardedUserInformation(user *user, name string, password string) (found bool, u *user, c *cluster, cu *clusterUser) {
	found = false
	c = rp.clusters[user.toCluster]
	wildcardedCu := c.users[user.toUser]
	if wildcardedCu != nil {
		newCU := deepCopy(wildcardedCu)
		found = true
		u = user
		cu = newCU
		cu.name = name
		cu.password = password

		// TODO : improve the following behavior
		// the wildcarded user feature creates some side-effects on clusterUser limitations (like the max_concurrent_queries)
		// because of the use of a deep copy of the clusterUser. The side effect should not be too impactful since the limitation still works on user.
		// But we need this deep copy since we're changing the name & password of clusterUser and if we used the same instance for every call to chproxy,
		// it could lead to security issues since a specific query run by user A on chproxy side could trigger a query in clickhouse from user B.
		// Doing a clean fix would require a huge refactoring.
	}
	return
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

	found, u, c, cu := rp.getUser(name, password)
	if !found {
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

	q, err := getFullQuery(req)
	if err != nil {
		return nil, http.StatusBadRequest, fmt.Errorf("%s: cannot read query: %w", s, err)
	}
	s.requestPacketSize = len(q)
	return s, 0, nil
}
