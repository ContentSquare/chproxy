package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
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
	label := prometheus.Labels{"user": scope.user.name, "target": scope.target.addr.Host}

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req = req.WithContext(ctx)

	rp.ReverseProxy.ServeHTTP(rw, req)

	select {
	case <- time.After(scope.user.limits.maxExecutionTime):
		cancel()
		respondWIthErr(rw, fmt.Errorf("Some err"))
		if err := killQuery(scope.user.name, scope.user.limits.maxExecutionTime.Seconds()); err != nil {
			log.Errorf("error while killing %q's queries: %s", scope.user.name, err)
		}
		errors.With(label).Inc()
	case <- ctx.Done():
		fmt.Println("succ")
		requestSuccess.With(label).Inc()
	}

	scope.dec()
	requestSum.With(label).Inc()
	log.Debugf("Request for scope %s successfully proxied", scope)
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

	users := make(map[string]*limits, len(cfg.Users))
	for _, user := range cfg.Users {
		users[user.Name] = &limits{
			maxConcurrentQueries: user.MaxConcurrentQueries,
			maxExecutionTime:     user.MaxExecutionTime,
		}
	}

	rp.targets = targets
	rp.users = users
	return nil
}

type reverseProxy struct {
	*httputil.ReverseProxy

	sync.Mutex
	users   map[string]*limits
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

	limits, ok := rp.users[name]
	if !ok {
		return nil, fmt.Errorf("unknown username %q", name)
	}

	return &user{
		name:   name,
		limits: limits,
	}, nil
}

func (rp *reverseProxy) getTarget() *target {
	rp.Lock()
	defer rp.Unlock()

	var idle *target
	for _, t := range rp.targets {
		if t.runningQueries == 0 {
			return t
		}

		if idle == nil || idle.runningQueries > t.runningQueries {
			idle = t
		}
	}

	return idle
}

type scope struct {
	user   *user
	target *target
}

func (s *scope) String() string {
	return fmt.Sprintf("[User: %s, running queries: %d => Host: %s, running queries: %d]",
		s.user.name, s.user.limits.runningQueries,
		s.target.addr.Host, s.target.runningQueries)
}

func (s *scope) inc() error {
	if err := s.user.limits.Inc(); err != nil {
		return fmt.Errorf("limits for user %q are exceeded: %s", s.user.name, err)
	}
	s.target.Inc()
	return nil
}

func (s *scope) dec() {
	s.user.limits.Dec()
	s.target.Dec()
}

type user struct {
	name   string
	limits *limits
}

type limits struct {
	maxConcurrentQueries uint32
	maxExecutionTime     time.Duration

	sync.Mutex
	runningQueries uint32
}

func (l *limits) Inc() error {
	l.Lock()
	defer l.Unlock()

	if l.maxConcurrentQueries > 0 && l.runningQueries >= l.maxConcurrentQueries {
		return fmt.Errorf("maxConcurrentQueries limit exceeded: %d", l.maxConcurrentQueries)
	}

	l.runningQueries++
	return nil
}

func (l *limits) Dec() {
	l.Lock()
	l.runningQueries--
	l.Unlock()
}

type target struct {
	addr *url.URL

	sync.Mutex
	runningQueries uint32
}

func (t *target) Inc() {
	t.Lock()
	t.runningQueries++
	t.Unlock()
}

func (t *target) Dec() {
	t.Lock()
	t.runningQueries--
	t.Unlock()
}

func respondWIthErr(rw http.ResponseWriter, err error) {
	log.Errorf("proxy failed: %s", err)
	rw.WriteHeader(http.StatusInternalServerError)
	rw.Write([]byte(err.Error()))
}

func extractUserFromRequest(req *http.Request) string {
	if name, _, ok := req.BasicAuth(); ok {
		return name
	}

	if name := req.Form.Get("user"); name != "" {
		return name
	}

	return "default"
}
