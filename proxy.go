package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"net"
	"time"

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
				Proxy: http.ProxyFromEnvironment,
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
	if err := rp.ApplyConfig(cfg); err != nil {
		return nil, err
	}

	return rp, nil
}

// Serves incoming requests according to config
func (rp *reverseProxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	log.Debugf("Accepting request: %v", req.Header)
	scope, err := rp.getRequestScope(req)
	if err != nil {
		respondWIthErr(rw, err)
		return
	}
	log.Debugf("Request scope is: %s", scope)

	if err = scope.inc(); err != nil {
		respondWIthErr(rw, err)
		return
	}

	ctx := context.Background()
	if scope.user.maxExecutionTime != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, scope.user.maxExecutionTime)
		defer cancel()
	}
	req = req.WithContext(ctx)

	label := prometheus.Labels{"user": scope.user.name, "target": scope.target.addr.Host}
	requestSum.With(label).Inc()

	rp.ReverseProxy.ServeHTTP(rw, req)

	if req.Context().Err() != nil {
		timeouts.With(label).Inc()
		rp.killQueries(scope.user.name, scope.user.maxExecutionTime.Seconds())
		message := fmt.Sprintf("timeout for user %q exceeded: %v", scope.user.name, scope.user.maxExecutionTime)
		fmt.Fprint(rw, message)
	} else {
		requestSuccess.With(label).Inc()
	}

	scope.dec()
	log.Debugf("Request for scope %s successfully proxied", scope)
}

// Reloads configuration from passed file
// return error if configuration is invalid
func (rp *reverseProxy) ReloadConfig(file string) error {
	cfg, err := config.LoadFile(file)
	if err != nil {
		return fmt.Errorf("can't load config %q: %s", file, err)
	}

	return rp.ApplyConfig(cfg)
}

// Applies provided config to reverseProxy
// New config will be applied only if non-nil error returned
func (rp *reverseProxy) ApplyConfig(cfg *config.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	rp.Lock()
	defer rp.Unlock()

	targets := make([]*target, len(cfg.Cluster.Shards))
	for i, t := range cfg.Cluster.Shards {
		addr, err := url.Parse(fmt.Sprintf("%s://%s", cfg.Cluster.Scheme, t))
		if err != nil {
			return err
		}

		targets[i] = &target{
			addr: addr,
		}
	}

	users := make(map[string]*user, len(cfg.Users))
	for _, u := range cfg.Users {
		users[u.Name] = &user{
			name:                 u.Name,
			maxConcurrentQueries: u.MaxConcurrentQueries,
			maxExecutionTime:     u.MaxExecutionTime,
		}
	}

	rp.targets = targets
	rp.users = users
	return nil
}

type reverseProxy struct {
	*httputil.ReverseProxy

	sync.Mutex
	users   map[string]*user
	targets []*target
}

func (rp *reverseProxy) getRequestScope(req *http.Request) (*scope, error) {
	user, err := rp.getUser(req)
	if err != nil {
		return nil, err
	}

	target := rp.getTarget()
	req.URL.Scheme = target.addr.Scheme
	req.URL.Host = target.addr.Host

	return &scope{
		user:   user,
		target: target,
	}, nil
}

func (rp *reverseProxy) getUser(req *http.Request) (*user, error) {
	name := extractUserFromRequest(req)

	rp.Lock()
	defer rp.Unlock()

	user, ok := rp.users[name]
	if !ok {
		return nil, fmt.Errorf("unknown username %q", name)
	}

	return user, nil
}

func (rp *reverseProxy) getTarget() *target {
	rp.Lock()
	defer rp.Unlock()

	var idle *target
	for _, t := range rp.targets {
		t.Lock()
		if t.runningQueries == 0 {
			t.Unlock()
			return t
		}

		if idle == nil || idle.runningQueries > t.runningQueries {
			idle = t
		}
		t.Unlock()
	}

	return idle
}

// We don't use query_id because of distributed processing, the query ID is not passed to remote servers
func (rp *reverseProxy) killQueries(user string, elapsed float64) {
	rp.Lock()
	addrs := make([]string, len(rp.targets))
	for i, target := range rp.targets {
		addrs[i] = target.addr.String()
	}
	rp.Unlock()

	q := fmt.Sprintf("KILL QUERY WHERE initial_user = '%s' AND elapsed >= %d", user, int(elapsed))
	for _, addr := range addrs {
		if err := doQuery(q, addr); err != nil {
			log.Errorf("error while killing queries older %.2fs than for user %q: %s", elapsed, user, err)
		}
	}
}

type observableTransport struct {
	http.Transport
}

func (pt *observableTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := pt.Transport.RoundTrip(r)
	if response != nil {
		statusCodes.With(
			prometheus.Labels{"target": r.URL.Host, "code": response.Status},
		).Inc()
	}

	if err != nil {
		errors.With(
			prometheus.Labels{"target": r.URL.Host, "message": err.Error()},
		).Inc()
	}

	return response, err
}