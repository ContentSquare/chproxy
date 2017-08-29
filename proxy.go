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
func NewReverseProxy(cfg *config.Config) (*reverseProxy, error) {
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
	err := rp.ApplyConfig(cfg)

	return rp, err
}

func (rp *reverseProxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	log.Debugf("Accepting request from %s: %s", req.RemoteAddr, req.URL.String())
	s, err := rp.getRequestScope(req)
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
	// set custom User-Agent for proper handling of killQuery func
	ua := fmt.Sprintf("ClickHouseProxy: %s", s.user.name)
	req.Header.Set("User-Agent", ua)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		rp.ReverseProxy.ServeHTTP(rw, req)
		done <- struct{}{}
	}()

	select {
	case <-s.user.timeout():
		cancel()
		<-done

		userTimeouts.With(prometheus.Labels{
			"host": s.host.addr.Host,
			"user": s.user.name,
		}).Inc()
		condition := fmt.Sprintf("http_user_agent = '%s'", ua)
		s.cluster.killQueries(condition, s.user.maxExecutionTime.Seconds())
		message := fmt.Sprintf("timeout for user %q exceeded: %v", s.user.name, s.user.maxExecutionTime)
		rw.Write([]byte(message))
	case <-s.clusterUser.timeout():
		cancel()
		<-done

		clusterTimeouts.With(prometheus.Labels{
			"host":         s.host.addr.Host,
			"cluster_user": s.clusterUser.name,
		}).Inc()
		condition := fmt.Sprintf("user = '%s'", s.clusterUser.name)
		s.cluster.killQueries(condition, s.clusterUser.maxExecutionTime.Seconds())
		message := fmt.Sprintf("timeout for cluster user %q exceeded: %v", s.clusterUser.name, s.clusterUser.maxExecutionTime)
		rw.Write([]byte(message))
	case <-done:
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
			clusterUsers[u.Name] = &clusterUser{
				name:                 u.Name,
				password:             u.Password,
				maxConcurrentQueries: u.MaxConcurrentQueries,
				maxExecutionTime:     u.MaxExecutionTime,
			}
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

		allowedIPs, err := parseNetworks(u.AllowedNetworks)
		if err != nil {
			return fmt.Errorf("error while parsing user %q `allowed_networks` field: %s", u.Name, err)
		}

		users[u.Name] = &user{
			clusterUser: clusterUser{
				name:                 u.Name,
				password:             u.Password,
				maxConcurrentQueries: u.MaxConcurrentQueries,
				maxExecutionTime:     u.MaxExecutionTime,
			},
			toCluster:  u.ToCluster,
			toUser:     u.ToUser,
			allowedIPs: allowedIPs,
		}
	}

	rp.Lock()
	rp.clusters = clusters
	rp.users = users
	rp.Unlock()

	return nil
}

type reverseProxy struct {
	*httputil.ReverseProxy

	sync.Mutex
	users    map[string]*user
	clusters map[string]*cluster
}

func (rp *reverseProxy) getRequestScope(req *http.Request) (*scope, error) {
	name, password := basicAuth(req)

	rp.Lock()
	defer rp.Unlock()

	u, ok := rp.users[name]
	if !ok {
		return nil, fmt.Errorf("invalid username or password for user %q", name)
	}

	if u.password != password {
		return nil, fmt.Errorf("invalid username or password for user %q", name)
	}

	if !u.isAllowedAddr(req.RemoteAddr) {
		return nil, fmt.Errorf("user %q is not allowed to access from %s", name, req.RemoteAddr)
	}

	c, ok := rp.clusters[u.toCluster]
	if !ok {
		return nil, fmt.Errorf("BUG: user %q matches to unknown cluster %q", u.name, u.toCluster)
	}

	cu, ok := c.users[u.toUser]
	if !ok {
		return nil, fmt.Errorf("BUG: user %q matches to unknown user %q at cluster %q", u.name, u.toUser, u.toCluster)
	}

	return newScope(u, cu, c), nil
}

type observableTransport struct {
	http.Transport
}

func (pt *observableTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := pt.Transport.RoundTrip(r)
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
