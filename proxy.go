package main

import (
	"net/url"
	"net/http"
	"time"
	"net/http/httputil"
	"context"
	"log"
	"strings"
	"sync"
	"github.com/hagen1778/chproxy/config"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
)

type reverseProxy struct {
	*httputil.ReverseProxy

	sync.Mutex
	scheme string
	users map[string]*limits
	targets []*target
}

type limits struct{
	maxConcurrentQueries uint32
	maxExecutionTime time.Duration

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

type target struct{
	addr *url.URL

	sync.Mutex
	runningQueries uint32
}

// todo: bench with race
func (rp *reverseProxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	fmt.Println("Serve: ", req.URL.Host)
	uname := extractUserFromRequest(req)
	limits, err := rp.getUserLimits(uname)
	if err != nil {
		log.Printf("proxy failed: %s", err)
		rw.WriteHeader(http.StatusInternalServerError)
		rw.Write([]byte(err.Error()))
		return
	}
	//label := prometheus.Labels{"user": uname}

	if err := limits.Inc(); err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		rw.Write([]byte(err.Error()))
		return
	}

	ctx := context.Background()
	if limits.maxExecutionTime != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, limits.maxExecutionTime)
		defer cancel()
	}
	req = req.WithContext(ctx)

	//requestSum.With(label).Inc()
	rp.ReverseProxy.ServeHTTP(rw, req)
	if ctx.Err() != nil {
		if err := killQuery(uname, limits.maxExecutionTime.Seconds()); err != nil {
			log.Println("Can't kill query:", err)
		}
		rw.Write([]byte(ctx.Err().Error()))
		//errors.With(label).Inc()
	}

	//requestSuccess.With(label).Inc()
	limits.Dec()
}

func (rp *reverseProxy) getUserLimits(name string) (*limits, error) {
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

	//if len(rp.targets) == 1 {
		return rp.targets[0]
	//}
}

func NewReverseProxy(cfg *config.Config) (*reverseProxy, error) {
	rp := &reverseProxy{}
	if err := rp.ApplyConfig(cfg); err != nil {
		return nil, err
	}

	director := func(req *http.Request) {

		target := rp.getTarget()
		req.URL.Scheme = target.addr.Scheme
		req.URL.Host = target.addr.Host
		req.URL.Path = singleJoiningSlash(target.addr.Path, req.URL.Path)
		fmt.Println("Director: ", req.URL.Host)
		if _, ok := req.Header["User-Agent"]; !ok {
			// explicitly disable User-Agent so it's not set to default value
			req.Header.Set("User-Agent", "")
		}
		requestSum.With(prometheus.Labels{"target": req.URL.Host}).Inc()
	}

	rp.ReverseProxy = &httputil.ReverseProxy{Director: director}
	initMetrics()

	return rp, nil
}

func (rp *reverseProxy) ApplyConfig(cfg *config.Config) error {
	rp.Lock()
	defer rp.Unlock()

	targets := make([]*target, len(cfg.Cluster.Shards))
	for i, t := range cfg.Cluster.Shards {
		addr, err := url.Parse(fmt.Sprintf("%s://%s", cfg.Cluster.Scheme, t))
		if err != nil {
			return err
		}

		addr.Scheme = cfg.Cluster.Scheme
		targets[i] = &target{
			addr:  addr,
		}
	}

	users := make(map[string]*limits, len(cfg.Users))
	for _, user := range cfg.Users {
		users[user.Name] = &limits{
			maxConcurrentQueries: user.MaxConcurrentQueries,
			maxExecutionTime: user.MaxExecutionTime,
		}
	}

	rp.targets = targets
	rp.users = users

	return nil
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

func extractUserFromRequest(r *http.Request) string {
	if uname, _, ok := r.BasicAuth(); ok {
		return uname
	}

	if uname := r.Form.Get("user"); uname != "" {
		return uname
	}

	return "default"
}
