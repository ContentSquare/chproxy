package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/contentsquare/chproxy/config"
)

var (
	hbHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			fmt.Fprintln(w, "Ok.")
			return
		}
		qid := r.URL.Query().Get("query")
		if len(qid) != 0 {
			fmt.Fprintln(w, "1")
			return
		}
		fmt.Fprintln(w, "wrong")
	})

	fakeHBServer = httptest.NewServer(hbHandler)

	clusterCfg = config.Cluster{
		Name:   "cluster",
		Scheme: "http",
		Replicas: []config.Replica{
			{
				Nodes: []string{fakeHBServer.URL},
			},
		},
		ClusterUsers: []config.ClusterUser{
			{
				Name:     "web",
				Password: "123",
			},
		},
		HeartBeat: config.HeartBeat{
			Interval: config.Duration(5 * time.Second),
			Timeout:  config.Duration(3 * time.Second),
			Request:  "/ping",
			Response: "Ok.\n",
		},
	}

	heartBeatFullCfg = config.HeartBeat{
		Interval: config.Duration(20 * time.Second),
		Timeout:  config.Duration(30 * time.Second),
		Request:  "/?query=SELECT%201",
		Response: "1\n",
	}

	heartBeatWrongResponseCfg = config.HeartBeat{
		Interval: config.Duration(20 * time.Second),
		Timeout:  config.Duration(30 * time.Second),
		Request:  "/wrongQuery",
		Response: "Ok.\n",
	}
)

func TestNewHeartBeat(t *testing.T) {
	c, err := newCluster(clusterCfg)
	if err != nil {
		t.Fatalf("error while initialize claster: %s", err)
	}
	testCompareNum(t, "cluster.heartbeat.interval", int64(c.heartBeat.interval/time.Microsecond), int64(time.Duration(5*time.Second)/time.Microsecond))

	hb := newHeartBeat(heartBeatFullCfg, clusterCfg.ClusterUsers[0])
	testCompareNum(t, "heartbeat.interval", int64(hb.interval/time.Microsecond), int64(time.Duration(20*time.Second)/time.Microsecond))
	testCompareNum(t, "heartbeat.timeout", int64(hb.timeout/time.Microsecond), int64(time.Duration(30*time.Second)/time.Microsecond))
	testCompareStr(t, "heartbeat.request", hb.request, "/?query=SELECT%201")
	testCompareStr(t, "heartbeat.response", hb.response, "1\n")
	testCompareStr(t, "heartbeat.user", hb.user, "web")
	testCompareStr(t, "heartbeat.password", hb.password, "123")
	if check := c.heartBeat.isHealthy(fakeHBServer.URL); check != nil {
		t.Fatalf("ping request error `%q`", check)
	}
	if check := hb.isHealthy(fakeHBServer.URL); check != nil {
		t.Fatalf("query request error `%q`", check)
	}

	hbWrong := newHeartBeat(heartBeatWrongResponseCfg, clusterCfg.ClusterUsers[0])
	check := hbWrong.isHealthy(fakeHBServer.URL)
	if check == nil {
		t.Fatalf("heartbeat error expected")
	}
	testCompareStr(t, "heartbeat error", check.Error(), "unexpected response: wrong\n")
}

func testCompareNum(t *testing.T, name string, value int64, expected int64) {
	t.Helper()
	if value != expected {
		t.Fatalf("expected %s for host: `%d`; got: `%d`", name, expected, value)
	}
}

func testCompareStr(t *testing.T, name string, value string, expected string) {
	t.Helper()
	if value != expected {
		t.Fatalf("expected %s for host: `%q`; got: `%q`", name, expected, value)
	}
}
