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
		ErrorLog: log.ErrorLogger,
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

	rp.ReverseProxy.ServeHTTP(rw, req)

	label := prometheus.Labels{"user": scope.user.name, "target": scope.target.addr.Host}
	requestSum.With(label).Inc()
	if req.Context().Err() != nil {
		fmt.Fprint(rw, fmt.Sprintf("timeout for user %q exceeded: %v", scope.user.name, scope.user.maxExecutionTime))
		if err := killQuery(scope.user.name, scope.user.maxExecutionTime.Seconds()); err != nil {
			log.Errorf("error while killing %q's queries: %s", scope.user.name, err)
		}
		errors.With(label).Inc()
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

type scope struct {
	user   *user
	target *target
}

func (s *scope) String() string {
	return fmt.Sprintf("[User: %s, running queries: %d => Host: %s, running queries: %d]",
		s.user.name, s.user.runningQueries,
		s.target.addr.Host, s.target.runningQueries)
}

func (s *scope) inc() error {
	if err := s.user.Inc(); err != nil {
		return fmt.Errorf("limits for user %q are exceeded: %s", s.user.name, err)
	}
	s.target.Inc()
	return nil
}

func (s *scope) dec() {
	s.user.Dec()
	s.target.Dec()
}

type user struct {
	name string

	sync.Mutex
	maxConcurrentQueries uint32
	maxExecutionTime     time.Duration
	runningQueries       uint32
}

func (u *user) Inc() error {
	u.Lock()
	defer u.Unlock()

	if u.maxConcurrentQueries > 0 && u.runningQueries >= u.maxConcurrentQueries {
		return fmt.Errorf("maxConcurrentQueries limit exceeded: %d", u.maxConcurrentQueries)
	}

	u.runningQueries++
	return nil
}

func (u *user) Dec() {
	u.Lock()
	u.runningQueries--
	u.Unlock()
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
