package main

import "testing"

func TestScope_RunningQueries(t *testing.T) {
	eu := &executionUser{
		maxConcurrentQueries: 1,
	}

	if err := eu.inc(); err != nil {
		t.Fatalf("unexpected err: %s", err)
	}

	if eu.runningQueries() != 1 {
		t.Fatalf("expected runningQueries: 1; got: %d", eu.runningQueries())
	}

	if err := eu.inc(); err == nil {
		t.Fatalf("error expected while call .inc()")
	}

	eu.dec()

	if err := eu.inc(); err != nil {
		t.Fatalf("unexpected err: %s", err)
	}
}
