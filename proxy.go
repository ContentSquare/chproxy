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

	"github.com/hagen1778/chproxy/config"
	"github.com/hagen1778/chproxy/log"
	"github.com/prometheus/client_golang/prometheus"
)

func newReverseProxy() *reverseProxy {
	return &reverseProxy{
		ReverseProxy: &httputil.ReverseProxy{
			Director: func(*http.Request) {},
			ErrorLog: log.ErrorLogger,
		},
	}
}

func (rp *reverseProxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	log.Debugf("Accepting request from %s: %s", req.RemoteAddr, req.URL)

	name, password := getAuth(req)
	s, err := rp.getRequestScope(req, name, password)
	if err != nil {
		respondWithErr(rw, err)
		return
	}
	log.Debugf("Request scope %s", s)

	if err = s.inc(); err != nil {
		respondWithErr(rw, err)
		return
	}
	defer s.dec()

	label := prometheus.Labels{
		"user":         s.user.name,
		"cluster_user": s.clusterUser.name,
		"host":         s.host.addr.Host,
	}
	requestSum.With(label).Inc()

	req.URL.Scheme = s.host.addr.Scheme
	req.URL.Host = s.host.addr.Host

	// override credentials before proxying request to CH
	setAuth(req, s.clusterUser.name, s.clusterUser.password)

	// set custom User-Agent for proper handling of killQuery func
	ua := fmt.Sprintf("CHProxy; User %s; Scope %d", s.user.name, s.id)
	req.Header.Set("User-Agent", ua)

	var (
		timeout        time.Duration
		timeoutCounter prometheus.Counter
		timeoutErrMsg  error
	)

	if s.user.maxExecutionTime > 0 {
		timeout = s.user.maxExecutionTime
		timeoutCounter = userTimeouts.With(prometheus.Labels{
			"host": s.host.addr.Host,
			"user": s.user.name,
		})
		timeoutErrMsg = fmt.Errorf("timeout for user %q exceeded: %v", s.user.name, timeout)
	}

	if timeout == 0 || (s.clusterUser.maxExecutionTime > 0 && s.clusterUser.maxExecutionTime < timeout) {
		timeout = s.clusterUser.maxExecutionTime
		timeoutCounter = clusterTimeouts.With(prometheus.Labels{
			"host": s.host.addr.Host,
			"user": s.clusterUser.name,
		})
		timeoutErrMsg = fmt.Errorf("timeout for cluster user %q exceeded: %v", s.clusterUser.name, timeout)
	}

	ctx := context.Background()
	if timeout != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	req = req.WithContext(ctx)
	cw := &cachedWriter{
		ResponseWriter: rw,
	}
	rp.ReverseProxy.ServeHTTP(cw, req)

	switch {
	case req.Context().Err() != nil:
		timeoutCounter.Inc()
		s.cluster.killQueries(ua, timeout.Seconds())
		rw.Write([]byte(timeoutErrMsg.Error()))
	case cw.statusCode == http.StatusOK:
		requestSuccess.With(label).Inc()
		log.Debugf("Request scope %s successfully proxied", s)
	case cw.statusCode == http.StatusBadGateway:
		requestErrors.With(label).Inc()
		b := []byte(fmt.Sprintf("unable to reach address: %s", req.URL.Host))
		rw.Write(b)
	default:
		requestErrors.With(label).Inc()
	}

	statusCodes.With(
		prometheus.Labels{"host": req.URL.Host, "code": strconv.Itoa(cw.statusCode)},
	).Inc()
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

			hosts[i] = &host{
				addr: addr,
			}
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
		clusters[c.Name] = newCluster(hosts, clusterUsers)
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
			allowedNetworks:      u.Networks,
			name:                 u.Name,
			password:             u.Password,
			maxConcurrentQueries: u.MaxConcurrentQueries,
			maxExecutionTime:     u.MaxExecutionTime,
		}
	}

	rp.mu.Lock()
	rp.clusters = clusters
	rp.users = users
	rp.mu.Unlock()

	return nil
}

type reverseProxy struct {
	*httputil.ReverseProxy

	mu       sync.Mutex
	users    map[string]*user
	clusters map[string]*cluster
}

func (rp *reverseProxy) getRequestScope(req *http.Request, name, password string) (*scope, error) {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	u, ok := rp.users[name]
	if !ok {
		return nil, fmt.Errorf("invalid username or password for user %q", name)
	}

	if u.password != password {
		return nil, fmt.Errorf("invalid username or password for user %q", name)
	}

	if !u.allowedNetworks.Contains(req.RemoteAddr) {
		return nil, fmt.Errorf("user %q is not allowed to access from %s", name, req.RemoteAddr)
	}

	c, ok := rp.clusters[u.toCluster]
	if !ok {
		panic(fmt.Sprintf("BUG: user %q matches to unknown cluster %q", u.name, u.toCluster))
	}

	cu, ok := c.users[u.toUser]
	if !ok {
		panic(fmt.Sprintf("BUG: user %q matches to unknown user %q at cluster %q", u.name, u.toUser, u.toCluster))
	}

	return newScope(u, cu, c), nil
}

type cachedWriter struct {
	http.ResponseWriter
	statusCode int
}

func (cw *cachedWriter) WriteHeader(code int) {
	cw.statusCode = code
	cw.ResponseWriter.WriteHeader(code)
}
