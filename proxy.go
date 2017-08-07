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
)

type reverseProxy struct {
	*httputil.ReverseProxy

	sync.Mutex
	users []*user
	targets []*target
}

type user struct{}
type target struct{}

func (rp *reverseProxy) ApplyConfig(cfg *config.Config) {

}

var deadline = time.Second*3

func (rp reverseProxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	startTime := time.Now()

	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	req = req.WithContext(ctx)

	uname := extractUserFromRequest(req)
	log.Println("user:", uname)

	rp.ServeHTTP(rw, req)

	if ctx.Err() != nil {
		if err := killQuery(uname, deadline.Seconds()); err != nil {
			log.Println("Can't kill query:", err)
		}
		rw.Write([]byte(ctx.Err().Error()))
	} else {
		log.Println("Request took", time.Since(startTime))
	}
}

func NewReverseProxy(target *url.URL) *reverseProxy {
	targetQuery := target.RawQuery
	director := func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = singleJoiningSlash(target.Path, req.URL.Path)
		if targetQuery == "" || req.URL.RawQuery == "" {
			req.URL.RawQuery = targetQuery + req.URL.RawQuery
		} else {
			req.URL.RawQuery = targetQuery + "&" + req.URL.RawQuery
		}
		if _, ok := req.Header["User-Agent"]; !ok {
			// explicitly disable User-Agent so it's not set to default value
			req.Header.Set("User-Agent", "")
		}
	}

	return &reverseProxy{
		ReverseProxy: &httputil.ReverseProxy{Director: director},
	}
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
