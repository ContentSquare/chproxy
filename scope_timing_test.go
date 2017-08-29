package main

import (
	"net/url"
	"testing"
)

func BenchmarkScope_RunningQueries(b *testing.B) {
	eu := &clusterUser{
		maxConcurrentQueries: 10,
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			eu.inc()
			eu.runningQueries()
			eu.dec()
		}
	})
}

func BenchmarkScope_GetHost(b *testing.B) {
	c := &cluster{
		hosts: []*host{
			{
				addr: &url.URL{Host: "127.0.0.1"},
			},
			{
				addr: &url.URL{Host: "127.0.0.2"},
			},
			{
				addr: &url.URL{Host: "127.0.0.3"},
			},
		},
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			h := c.getHost()
			h.inc()
			h.dec()
		}
	})
}
