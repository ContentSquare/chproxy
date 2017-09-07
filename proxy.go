package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"net"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/hagen1778/chproxy/config"
	"github.com/hagen1778/chproxy/log"
	"github.com/prometheus/client_golang/prometheus"
)

// Creates new reverseProxy with provided config
func NewReverseProxy() *reverseProxy {
	rp := &reverseProxy{}
	rp.ReverseProxy = &httputil.ReverseProxy{
		Director: func(*http.Request) {},
		ErrorLog: log.ErrorLogger,
		Transport: &observableTransport{
			http.Transport{
				DialContext: (&net.Dialer{
					KeepAlive: 30 * time.Second,
					DualStack: true,
				}).DialContext,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		},
	}
	return rp
}

func (rp *reverseProxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	log.Debugf("Accepting request from %s: %s", req.RemoteAddr, req.URL)
	cw := newCachedWriter(rw)
	s, err := rp.getRequestScope(req)
	if err != nil {
		log.Errorf("proxy failed: %s", err)
		cw.WriteError(err, http.StatusInternalServerError)
		return
	}
	log.Debugf("Request scope %s", s)

	if err = s.inc(); err != nil {
		log.Errorf("proxy failed: %s", err)
		cw.WriteError(err, http.StatusInternalServerError)
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
	// set custom User-Agent for proper handling of killQuery func
	ua := fmt.Sprintf("ClickHouseProxy: %s", s.user.name)
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
	rp.ReverseProxy.ServeHTTP(cw, req)

	switch {
	case req.Context().Err() != nil:
		timeoutCounter.Inc()
		s.cluster.killQueries(ua, timeout.Seconds())
		cw.WriteError(timeoutErrMsg, http.StatusRequestTimeout)
	case cw.Status() != http.StatusOK:
		//TODO: counter err inc?
		err = fmt.Errorf("Proxy error: %s", cw.wbuf.String())
		cw.WriteError(err, cw.Status())
	default:
		requestSuccess.With(label).Inc()
	}

	log.Debugf("Request scope %s successfully proxied", s)
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

func (rp *reverseProxy) getRequestScope(req *http.Request) (*scope, error) {
	name, password := basicAuth(req)

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

type observableTransport struct {
	http.Transport
}

func (ot *observableTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := ot.Transport.RoundTrip(r)
	if response != nil {
		statusCodes.With(
			prometheus.Labels{"host": r.URL.Host, "code": response.Status},
		).Inc()
	}

	if err != nil {
		errors.With(
			prometheus.Labels{"host": r.URL.Host, "message": err.Error()},
		).Inc()
	}

	return response, err
}
