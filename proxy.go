package main

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/Vertamedia/chproxy/config"
	"github.com/Vertamedia/chproxy/log"
	"github.com/prometheus/client_golang/prometheus"
)

func newReverseProxy() *reverseProxy {
	return &reverseProxy{
		ReverseProxy: &httputil.ReverseProxy{
			Director: func(*http.Request) {},
			ErrorLog: log.ErrorLogger,
		},
		reloadSignal: make(chan struct{}),
		reloadWG:     sync.WaitGroup{},
	}
}

func (rp *reverseProxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	log.Debugf("Accepting request from %s: %s", req.RemoteAddr, req.URL)

	s, err := rp.getScope(req)
	if err != nil {
		respondWithErr(rw, err)
		return
	}
	log.Debugf("Request scope %s", s)

	label := prometheus.Labels{
		"user":         s.user.name,
		"host":         s.host.addr.Host,
		"cluster_user": s.clusterUser.name,
	}
	requestSum.With(label).Inc()

	if err = s.inc(); err != nil {
		limitExcess.With(label).Inc()
		respondWith(rw, err, http.StatusTooManyRequests)
		return
	}
	defer s.dec()

	timeStart := time.Now()
	req = s.decorateRequest(req)

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
	ctx := context.Background()
	if timeout != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	req = req.WithContext(ctx)
	cw := &cachedWriter{ResponseWriter: rw}
	rp.ReverseProxy.ServeHTTP(cw, req)

	if req.Context().Err() != nil {
		// penalize host if respond is slow, probably it is overloaded
		s.host.penalize()
		cw.statusCode = http.StatusGatewayTimeout
		if err := s.killQuery(); err != nil {
			log.Errorf("error while killing query: %s", err)
		}
		fmt.Fprint(rw, timeoutErrMsg.Error())
	} else {
		switch cw.statusCode {
		case http.StatusOK:
			requestSuccess.With(label).Inc()
			log.Debugf("Request scope %s successfully proxied", s)
		case http.StatusBadGateway:
			s.host.penalize()
			fmt.Fprintf(rw, "unable to reach address: %s", req.URL.Host)
		}
	}
	statusCodes.With(
		prometheus.Labels{
			"user":         s.user.name,
			"cluster_user": s.clusterUser.name,
			"host":         s.host.addr.Host,
			"code":         strconv.Itoa(cw.statusCode),
		},
	).Inc()
	since := float64(time.Since(timeStart).Seconds())
	requestDuration.With(label).Observe(since)
}

// ApplyConfig applies provided config to reverseProxy obj
// New config will be applied only if non-nil error returned
// Otherwise old version will be kept
func (rp *reverseProxy) ApplyConfig(cfg *config.Config) error {
	clusters := make(map[string]*cluster, len(cfg.Clusters))
	for _, c := range cfg.Clusters {
		hosts := make([]*host, len(c.Nodes))
		for i, node := range c.Nodes {
			addr, err := url.Parse(fmt.Sprintf("%s://%s", c.Scheme, node))
			if err != nil {
				return err
			}
			hosts[i] = &host{addr: addr}
		}

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
			hosts:                 hosts,
			users:                 clusterUsers,
			heartBeatInterval:     c.HeartBeatInterval,
			killQueryUserName:     c.KillQueryUser.Name,
			killQueryUserPassword: c.KillQueryUser.Password,
		}
		clusters[c.Name] = cluster
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
		users[u.Name] = &user{
			name:                 u.Name,
			password:             u.Password,
			toUser:               u.ToUser,
			denyHTTP:             u.DenyHTTP,
			denyHTTPS:            u.DenyHTTPS,
			toCluster:            u.ToCluster,
			reqPerMin:            u.ReqPerMin,
			allowedNetworks:      u.AllowedNetworks,
			maxExecutionTime:     u.MaxExecutionTime,
			maxConcurrentQueries: u.MaxConcurrentQueries,
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
	for k, c := range clusters {
		for _, host := range c.hosts {
			h := host
			label := prometheus.Labels{
				"cluster": k,
				"host":    h.addr.Host,
			}
			rp.reloadWG.Add(1)
			go func() {
				h.runHeartbeat(c.heartBeatInterval, label, rp.reloadSignal)
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

type reverseProxy struct {
	*httputil.ReverseProxy

	reloadSignal chan struct{}
	reloadWG     sync.WaitGroup

	mu       sync.RWMutex
	users    map[string]*user
	clusters map[string]*cluster
}

func (rp *reverseProxy) getScope(req *http.Request) (*scope, error) {
	name, password := getAuth(req)

	rp.mu.RLock()
	defer rp.mu.RUnlock()

	u, ok := rp.users[name]
	if !ok {
		return nil, fmt.Errorf("invalid username or password for user %q", name)
	}
	if u.password != password {
		return nil, fmt.Errorf("invalid username or password for user %q", name)
	}
	c, ok := rp.clusters[u.toCluster]
	if !ok {
		panic(fmt.Sprintf("BUG: user %q matches to unknown cluster %q", u.name, u.toCluster))
	}
	cu, ok := c.users[u.toUser]
	if !ok {
		panic(fmt.Sprintf("BUG: user %q matches to unknown user %q at cluster %q", u.name, u.toUser, u.toCluster))
	}
	if u.denyHTTP && req.URL.Scheme == "http" {
		return nil, fmt.Errorf("user %q is not allowed to access via http", u.name)
	}
	if u.denyHTTP && req.URL.Scheme == "https" {
		return nil, fmt.Errorf("user %q is not allowed to access via https", u.name)
	}
	if !u.allowedNetworks.Contains(req.RemoteAddr) {
		return nil, fmt.Errorf("user %q is not allowed to access from %s", u.name, req.RemoteAddr)
	}
	if !cu.allowedNetworks.Contains(req.RemoteAddr) {
		return nil, fmt.Errorf("cluster user %q is not allowed to access from %s", cu.name, req.RemoteAddr)
	}
	s, err := newScope(u, cu, c)
	if err != nil {
		return nil, fmt.Errorf("error while creating scope for cluster %q: %s", u.toCluster, err)
	}
	return s, nil
}

// cache writer suppose to intercept headers set
type cachedWriter struct {
	http.ResponseWriter
	statusCode int
}

func (cw *cachedWriter) WriteHeader(code int) {
	cw.statusCode = code
	cw.ResponseWriter.WriteHeader(code)
}
