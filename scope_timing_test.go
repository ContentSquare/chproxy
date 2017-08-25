package main

import "testing"

func BenchmarkScope_RunningQueries(b *testing.B) {
	eu := &executionUser{
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
