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
		stopCheckers: make(chan struct{}),
	}
}

func (rp *reverseProxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	log.Debugf("Accepting request from %s: %s", req.RemoteAddr, req.URL)

	name, password := getAuth(req)
	s, err := rp.getScope(name, password)
	if err != nil {
		respondWithErr(rw, err)
		return
	}
	log.Debugf("Request scope %s", s)

	if s.user.deny_http && req.URL.Scheme == "http" {
		respondWithErr(rw, fmt.Errorf("user %q is not allowed to access via http", name))
		return
	}

	if s.user.deny_https && req.URL.Scheme == "https" {
		respondWithErr(rw, fmt.Errorf("user %q is not allowed to access via https", name))
		return
	}

	if !s.user.allowedNetworks.Contains(req.RemoteAddr) {
		respondWithErr(rw, fmt.Errorf("user %q is not allowed to access from %s", name, req.RemoteAddr))
		return
	}

	label := prometheus.Labels{
		"user":         s.user.name,
		"host":         s.host.addr.Host,
		"cluster_user": s.clusterUser.name,
	}
	requestSum.With(label).Inc()

	if err = s.inc(); err != nil {
		concurrentLimitExcess.With(label).Inc()
		respondWithErr(rw, err)
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
		cw.statusCode = http.StatusGatewayTimeout
		if err := s.killQuery(); err != nil {
			log.Errorf("error while killing query: %s", err)
		}
		s.host.penalize()
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

// Applies provided config to reverseProxy
// New config will be applied only if non-nil error returned
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
				maxConcurrentQueries: u.MaxConcurrentQueries,
				maxExecutionTime:     u.MaxExecutionTime,
			}
		}

		if _, ok := clusters[c.Name]; ok {
			return fmt.Errorf("cluster %q already exists", c.Name)
		}
		cluster := &cluster{
			hosts:             hosts,
			users:             clusterUsers,
			heartBeatInterval: c.HeartBeatInterval,
		}
		cluster.killQueryUserName = c.KillQueryUser.Name
		cluster.killQueryUserPassword = c.KillQueryUser.Password
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
			toCluster:            u.ToCluster,
			toUser:               u.ToUser,
			deny_http:            u.DenyHTTP,
			deny_https:           u.DenyHTTPS,
			allowedNetworks:      u.AllowedNetworks,
			name:                 u.Name,
			password:             u.Password,
			maxConcurrentQueries: u.MaxConcurrentQueries,
			maxExecutionTime:     u.MaxExecutionTime,
		}
	}

	// if we are here then there is no errors with config
	// let's stop health-checking goroutines and reset metrics
	close(rp.stopCheckers)
	// wait while all goroutines will stop
	rp.checkersDone.Wait()
	hostHealth.Reset()

	// run health-checking for new configuration
	rp.stopCheckers = make(chan struct{})
	for k, c := range clusters {
		for _, host := range c.hosts {
			h := host
			go func() {
				rp.checkersDone.Add(1)
				h.runHeartbeat(c.heartBeatInterval, k, rp.stopCheckers)
				rp.checkersDone.Done()
			}()
		}
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

	stopCheckers chan struct{}
	checkersDone sync.WaitGroup

	mu       sync.Mutex
	users    map[string]*user
	clusters map[string]*cluster
}

func (rp *reverseProxy) getScope(name, password string) (*scope, error) {
	rp.mu.Lock()
	defer rp.mu.Unlock()

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

	s, err := newScope(u, cu, c)
	if err != nil {
		return nil, fmt.Errorf("error while creating scope for cluster %q: %s", u.toCluster, err)
	}

	return s, nil
}

type cachedWriter struct {
	http.ResponseWriter
	statusCode int
}

func (cw *cachedWriter) WriteHeader(code int) {
	cw.statusCode = code
	cw.ResponseWriter.WriteHeader(code)
}
