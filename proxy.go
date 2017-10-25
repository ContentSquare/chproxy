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
			// @valyala: do we actually need error messages from ReverseProxy?
			ErrorLog:  log.ErrorLogger,
			Transport: &cacheTransport{http.DefaultTransport},
		},
		reloadSignal: make(chan struct{}),
		reloadWG:     sync.WaitGroup{},
	}
}

func (rp *reverseProxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	s, status, err := rp.getScope(req)
	if err != nil {
		err = fmt.Errorf("scope error for %q: %s", req.RemoteAddr, err)
		respondWith(rw, err, status)
		return
	}

	log.Debugf("Request scope %s", s)
	requestSum.With(s.labels).Inc()

	req.Body = &readCloser{
		ReadCloser:       req.Body,
		requestBodyBytes: requestBodyBytes.With(s.labels),
	}
	srw := &statResponseWriter{
		ResponseWriter:    rw,
		responseBodyBytes: responseBodyBytes.With(s.labels),
	}

	q := fetchQuery(req)
	if err = s.inc(); err != nil {
		limitExcess.With(s.labels).Inc()
		err = fmt.Errorf("%s for query %q", err, string(q))
		respondWith(srw, err, http.StatusTooManyRequests)
		return
	}
	defer s.dec()

	if s.user.allowCORS {
		origin := req.Header.Get("Origin")
		if len(origin) == 0 {
			origin = "*"
		}
		srw.Header().Set("Access-Control-Allow-Origin", origin)
	}

	var key string
	if s.cache != nil {
		key = cache.GenerateKey(q)
	}
	resp, ok := s.getFromCache(key)
	if ok {
		//just write cached response into connection
		srw.Write(resp)
	} else {
		timeStart := time.Now()
		req = s.decorateRequest(req)
		timeout, timeoutErrMsg := s.getTimeoutWithErrMsg()
		ctx := context.Background()
		if timeout != 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		req = req.WithContext(ctx)
		if s.cache != nil {
			crw := &cachedResponseWriter{
				cc:                 s.cache,
				key:                key,
				statResponseWriter: *srw,
			}
			rp.ReverseProxy.ServeHTTP(crw, req)
		} else {
			rp.ReverseProxy.ServeHTTP(srw, req)
		}
		if req.Context().Err() != nil {
			// penalize host if responding is slow, probably it is overloaded
			s.host.penalize()
			if err := s.killQuery(); err != nil {
				log.Errorf("error while killing query: %s", err)
			}
			log.Errorf("node %q: %s in query: %q", s.host.addr, timeoutErrMsg, string(q))
			fmt.Fprint(srw, timeoutErrMsg.Error())
		}
		since := float64(time.Since(timeStart).Seconds())
		requestDuration.With(s.labels).Observe(since)
	}

	switch srw.statusCode {
	case http.StatusOK:
		requestSuccess.With(s.labels).Inc()
		log.Debugf("Request scope %s successfully proxied", s)
	case http.StatusBadGateway:
		s.host.penalize()
		fmt.Fprintf(srw, "unable to reach address: %s", req.URL.Host)
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
}

// ApplyConfig applies provided config to reverseProxy obj
// New config will be applied only if non-nil error returned
// Otherwise old version will be kept
func (rp *reverseProxy) ApplyConfig(cfg *config.Config) error {
	clusters := make(map[string]*cluster, len(cfg.Clusters))
	for _, c := range cfg.Clusters {
		clusterUsers := make(map[string]*clusterUser, len(c.ClusterUsers))
		for _, u := range c.ClusterUsers {
			if _, ok := clusterUsers[u.Name]; ok {
				return fmt.Errorf("cluster user %q already exists", u.Name)
			}
			clusterUsers[u.Name] = &clusterUser{
				name:                 u.Name,
				password:             u.Password,
				reqPerMin:            u.ReqPerMin,
				allowedNetworks:      u.AllowedNetworks,
				maxExecutionTime:     u.MaxExecutionTime,
				maxConcurrentQueries: u.MaxConcurrentQueries,
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

	cc := cacheControllers.Load().(ccList)
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
		users[u.Name] = &user{
			name:                 u.Name,
			password:             u.Password,
			toUser:               u.ToUser,
			denyHTTP:             u.DenyHTTP,
			denyHTTPS:            u.DenyHTTPS,
			allowCORS:            u.AllowCORS,
			toCluster:            u.ToCluster,
			reqPerMin:            u.ReqPerMin,
			allowedNetworks:      u.AllowedNetworks,
			maxExecutionTime:     u.MaxExecutionTime,
			maxConcurrentQueries: u.MaxConcurrentQueries,
		}
		if len(u.Cache) > 0 {
			c := cache.GetController(u.Cache)
			if c == nil {
				return fmt.Errorf("there is no such cache %q for user %q ", u.Cache, u.Name)
			}
			cc[u.Name] = c
		}
	}
	cacheControllers.Store(cc)

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

	s := newScope()
	s.host = h
	s.cluster = c
	s.user = u
	s.clusterUser = cu
	s.remoteAddr = req.RemoteAddr
	s.cache = cacheControllers.Load().(ccList)[u.name]
	if addr, ok := req.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		s.localAddr = addr.String()
	}
	s.labels = prometheus.Labels{
		"user":         s.user.name,
		"cluster":      s.cluster.name,
		"cluster_user": s.clusterUser.name,
		"cluster_node": s.host.addr.Host,
	}

	return s, 0, nil
}

type cacheTransport struct {
	http.RoundTripper
}

func (ct *cacheTransport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	// get query from request before RoundTrip to save body
	query := getQuery(req)
	resp, err = ct.RoundTripper.RoundTrip(req)
	if err != nil {
		return
	}
	// cache only Ok responses
	if resp.StatusCode != http.StatusOK || resp.Body == nil {
		return
	}
	cc := cacheControllers.Load().(ccList)
	if cc == nil {
		return
	}
	name, _ := getAuth(resp.Request)
	c, ok := cc[name]
	if !ok {
		return
	}
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}
	key := cache.GenerateKey(query)
	// do not wait for slow creating/writing into file
	go c.Store(key, b)
	// restore body since we've already read&closed it
	resp.Body = ioutil.NopCloser(bytes.NewBuffer(b))
	return
}
