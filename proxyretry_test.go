package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/contentsquare/chproxy/internal/topology"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

type mockResponseWriterWithCode struct {
	http.ResponseWriter
	statusCode int
}

type mockStatResponseWriter struct {
	http.ResponseWriter
	http.CloseNotifier
	statusCode int
}

type mockHosts struct {
	t   *testing.T
	b   string
	hs  []string
	hst []string
}

// TestQueryWithRetryFail function statResponseWriter's statusCode will not be 200, but the query has been proxied
// The request will be retried 1 time with a different host in the same replica
func TestQueryWithRetryFail(t *testing.T) {
	body := "foo query"

	req := newRequest("http://localhost:8080", body)

	mhs := &mockHosts{
		t:  t,
		b:  body,
		hs: []string{"localhost:8080", "localhost:8081"},
	}

	s := newMockScope(mhs.hs)

	srw := mockStatRW(s)

	mrw := &mockResponseWriterWithCode{
		statusCode: 0,
	}

	retryNum := 1

	_, err := executeWithRetry(
		context.Background(),
		s,
		retryNum,
		mhs.mockReverseProxy,
		mrw,
		srw,
		req,
		func(f float64) {},
		func(l prometheus.Labels) {},
	)

	if err != nil {
		t.Errorf("the execution with retry failed: %v", err)
	}
	assert.Equal(t, srw.statusCode, http.StatusBadGateway)
	assert.Equal(t, mhs.hs, mhs.hst)

}

// TestRunQuerySuccessOnce function statResponseWriter's statusCode will be StatusOK after executeWithRetry, the query has been proxied
// The execution will succeeded without retry
func TestQuerySuccessOnce(t *testing.T) {
	body := "foo query"

	req := newRequest("http://localhost:8090", body)

	mhs := &mockHosts{
		t:  t,
		b:  body,
		hs: []string{"localhost:8080", "localhost:8090"},
	}

	s := newMockScope(mhs.hs)

	srw := mockStatRW(s)

	mrw := &mockResponseWriterWithCode{
		statusCode: 0,
	}

	retryNum := 1

	_, err := executeWithRetry(
		context.Background(),
		s,
		retryNum,
		mhs.mockReverseProxy,
		mrw,
		srw,
		req,
		func(f float64) {},
		func(l prometheus.Labels) {},
	)
	if err != nil {
		t.Errorf("The execution with retry failed, %v", err)
	}
	assert.Equal(t, mhs.hst, []string{mhs.hs[1]})
	assert.Equal(t, srw.statusCode, 200)
}

// TestQueryWithRetrySuccess function statResponseWriter's statusCode will be StatusOK after executeWithRetry, the query has been proxied
// The execution will succeeded after retry
func TestQueryWithRetrySuccess(t *testing.T) {
	body := "foo query"

	req := newRequest("http://localhost:8080", body)

	mhs := &mockHosts{
		t:  t,
		b:  body,
		hs: []string{"localhost:8080", "localhost:8090"},
	}

	s := newMockScope(mhs.hs)

	srw := mockStatRW(s)

	mrw := &mockResponseWriterWithCode{
		statusCode: 0,
	}

	retryNum := 1

	erroredHost := s.host

	_, err := executeWithRetry(
		context.Background(),
		s,
		retryNum,
		mhs.mockReverseProxy,
		mrw,
		srw,
		req,
		func(f float64) {},
		func(l prometheus.Labels) {},
	)
	if err != nil {
		t.Errorf("The execution with retry failed, %v", err)
	}
	assert.Equal(t, 200, srw.statusCode)
	assert.Equal(t, 1, int(s.host.CurrentConnections()))
	assert.Equal(t, 0, int(s.host.CurrentPenalty()))
	// should be counter + penalty
	assert.Equal(t, 1, int(s.host.CurrentLoad()))

	assert.Equal(t, 0, int(erroredHost.CurrentConnections()))
	assert.Equal(t, topology.DefaultPenaltySize, int(erroredHost.CurrentPenalty()))
	// should be counter + penalty
	assert.Equal(t, topology.DefaultPenaltySize, int(erroredHost.CurrentLoad()))

	assert.Equal(t, mhs.hs, mhs.hst)
}

func (mhs *mockHosts) mockReverseProxy(rw http.ResponseWriter, req *http.Request) {
	if req.URL.Host != "localhost:8090" {
		rw.WriteHeader(http.StatusBadGateway)
	} else {
		rw.WriteHeader(http.StatusOK)
	}

	b, err := io.ReadAll(req.Body)
	if err != nil {
		mhs.t.Errorf("The req body cannot be read: %v", err)
	}
	req.Body.Close()

	assert.Equal(mhs.t, mhs.b, string(b))

	mhs.hst = append(mhs.hst, req.URL.Host)
}

func newRequest(host, body string) *http.Request {
	// create a new req
	req := httptest.NewRequest(http.MethodPost, host, strings.NewReader(body))

	ctx := context.Background()

	req = req.WithContext(ctx)

	return req
}

func newHostsCluster(hs []string) *cluster {
	// set up cluster, replicas, hosts
	cluster1 := &cluster{
		name: "cluster1",
	}

	var hosts []*topology.Node

	replica1 := &replica{
		cluster:     cluster1,
		name:        "replica1",
		nextHostIdx: 0,
	}

	cluster1.replicas = []*replica{replica1}

	for i := 0; i < len(hs); i++ {
		url1 := &url.URL{
			Scheme: "http",
			Host:   hs[i],
		}
		hosti := topology.NewNode(url1, nil, "", replica1.name)
		hosti.SetIsActive(true)

		hosts = append(hosts, hosti)
	}

	replica1.hosts = hosts

	return cluster1
}

func newMockScope(hs []string) *scope {
	c := newHostsCluster(hs)
	scopedHost := c.replicas[0].hosts[0]
	scopedHost.IncrementConnections()

	return &scope{
		startTime: time.Now(),
		host:      scopedHost,
		cluster:   c,
		labels: prometheus.Labels{
			"user":         "default",
			"cluster":      "default",
			"cluster_user": "default",
			"replica":      "default",
			"cluster_node": "default",
		},
	}
}

func mockStatRW(s *scope) *mockStatResponseWriter {

	return &mockStatResponseWriter{
		ResponseWriter: httptest.NewRecorder(),
		CloseNotifier:  &testCloseNotifier{},
		statusCode:     0,
	}
}

func (srw *mockStatResponseWriter) StatusCode() int {
	return srw.statusCode
}

func (srw *mockStatResponseWriter) SetStatusCode(code int) {
	srw.statusCode = code
}

func (srw *mockStatResponseWriter) Header() http.Header {
	return srw.ResponseWriter.Header()
}

func (srw *mockStatResponseWriter) Write(i []byte) (int, error) {
	return srw.ResponseWriter.Write(i)
}

func (m *mockResponseWriterWithCode) StatusCode() int {
	return m.statusCode
}

func (m *mockResponseWriterWithCode) Header() http.Header {
	return m.ResponseWriter.Header()
}

func (m *mockResponseWriterWithCode) Write(i []byte) (int, error) {
	return m.ResponseWriter.Write(i)
}

func (m *mockResponseWriterWithCode) WriteHeader(statusCode int) {
	m.statusCode = statusCode
}
