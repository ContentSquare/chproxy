package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestReRunFail(t *testing.T) {
	//create a new req
	req := httptest.NewRequest(http.MethodGet, "http://localhost:8080", nil)

	ctx := context.Background()

	req = req.WithContext(ctx)

	// set up cluster, replica, hosts
	url1 := &url.URL{
		Scheme: "http",
		Host:   "localhost:8080",
	}

	url2 := &url.URL{
		Scheme: "http",
		Host:   "localhost:8081",
	}
	cluster1 := &cluster{
		name: "cluster1",
	}

	replica1 := &replica{
		cluster:     cluster1,
		name:        "replica1",
		nextHostIdx: 0,
	}

	replica2 := &replica{
		cluster:     cluster1,
		name:        "replica2",
		nextHostIdx: 0,
	}

	cluster1.replicas = []*replica{replica1, replica2}

	host1 := &host{
		replica: replica1,
		penalty: 1000,
		active:  1,
		addr:    url1,
	}

	host2 := &host{
		replica: replica1,
		penalty: 1000,
		active:  1,
		addr:    url2,
	}

	replica1.hosts = []*host{host1, host2}

	startTime := time.Now()

	s := &scope{
		startTime: startTime,
		host:      host1,
		cluster:   cluster1,
		labels: prometheus.Labels{
			"user":         "default",
			"cluster":      cluster1.name,
			"cluster_user": "default",
			"replica":      host1.replica.name,
			"cluster_node": host1.addr.Host,
		},
	}

	rw := httptest.NewRecorder()

	srw := &statResponseWriter{
		ResponseWriter: rw,
	}

	mrw := &mockResponseWriter{
		ResponseWriter: rw,
	}

	var err error

	retryNum := 1

	err1 := runReq(ctx, s, startTime, retryNum, mockReverseProxy, mrw, srw, req, err)

	if err1 == nil {
		t.Errorf("the retry should be failed: %v", err)
	}
}

func TestReRunOnce(t *testing.T) {
	//create a new req
	req := httptest.NewRequest(http.MethodGet, "http://localhost:8090", nil)

	ctx := context.Background()

	req = req.WithContext(ctx)

	// set up cluster, replica, hosts
	url1 := &url.URL{
		Scheme: "http",
		Host:   "localhost:8090",
	}

	url2 := &url.URL{
		Scheme: "http",
		Host:   "localhost:8080",
	}
	cluster1 := &cluster{
		name: "cluster1",
	}

	replica1 := &replica{
		cluster:     cluster1,
		name:        "replica1",
		nextHostIdx: 0,
	}

	replica2 := &replica{
		cluster:     cluster1,
		name:        "replica2",
		nextHostIdx: 0,
	}

	cluster1.replicas = []*replica{replica1, replica2}

	host1 := &host{
		replica: replica1,
		penalty: 1000,
		active:  1,
		addr:    url1,
	}

	host2 := &host{
		replica: replica1,
		penalty: 1000,
		active:  1,
		addr:    url2,
	}

	replica1.hosts = []*host{host1, host2}

	startTime := time.Now()

	s := &scope{
		startTime: startTime,
		host:      host1,
		cluster:   cluster1,
		labels: prometheus.Labels{
			"user":         "default",
			"cluster":      cluster1.name,
			"cluster_user": "default",
			"replica":      host1.replica.name,
			"cluster_node": host1.addr.Host,
		},
	}

	rw := httptest.NewRecorder()

	srw := &statResponseWriter{
		ResponseWriter: rw,
	}

	mrw := &mockResponseWriter{
		ResponseWriter: rw,
	}

	var err error

	retryNum := 1

	err1 := runReq(ctx, s, startTime, retryNum, mockReverseProxy, mrw, srw, req, err)

	if err1 != nil {
		t.Errorf("the retry is failed: %v", err)
	}
}

func TestReRun(t *testing.T) {
	//create a new req
	req := httptest.NewRequest(http.MethodGet, "http://localhost:8080", nil)

	ctx := context.Background()

	req = req.WithContext(ctx)

	// set up cluster, replica, hosts
	url1 := &url.URL{
		Scheme: "http",
		Host:   "localhost:8080",
	}

	url2 := &url.URL{
		Scheme: "http",
		Host:   "localhost:8090",
	}
	cluster1 := &cluster{
		name: "cluster1",
	}

	replica1 := &replica{
		cluster:     cluster1,
		name:        "replica1",
		nextHostIdx: 0,
	}

	replica2 := &replica{
		cluster:     cluster1,
		name:        "replica2",
		nextHostIdx: 0,
	}

	cluster1.replicas = []*replica{replica1, replica2}

	host1 := &host{
		replica: replica1,
		penalty: 1000,
		active:  1,
		addr:    url1,
	}

	host2 := &host{
		replica: replica1,
		penalty: 1000,
		active:  1,
		addr:    url2,
	}

	replica1.hosts = []*host{host1, host2}

	startTime := time.Now()

	s := &scope{
		startTime: startTime,
		host:      host1,
		cluster:   cluster1,
		labels: prometheus.Labels{
			"user":         "default",
			"cluster":      cluster1.name,
			"cluster_user": "default",
			"replica":      host1.replica.name,
			"cluster_node": host1.addr.Host,
		},
	}

	rw := httptest.NewRecorder()

	srw := &statResponseWriter{
		ResponseWriter: rw,
	}

	mrw := &mockResponseWriter{
		ResponseWriter: rw,
	}

	var err error

	retryNum := 1

	err1 := runReq(ctx, s, startTime, retryNum, mockReverseProxy, mrw, srw, req, err)

	if err1 != nil {
		t.Errorf("the retry is failed: %v", err)
	}
}

func mockReverseProxy(rw http.ResponseWriter, req *http.Request) {
	if req.URL.Host != "localhost:8090" {
		fmt.Println("unvalid host")
		rw.WriteHeader(http.StatusBadGateway)
	} else {
		fmt.Println("valid host")
		rw.WriteHeader(http.StatusOK)
	}
}
