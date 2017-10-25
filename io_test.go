package main

import (
	"bytes"
	"io/ioutil"
	"testing"
)

func TestReadFirstN(t *testing.T) {
	b := makeQuery(100)

	rc := &readCloser{
		ReadCloser:       ioutil.NopCloser(bytes.NewReader(b)),
		requestBodyBytes: badRequest,
	}
	var qLength = 10
	res, err := rc.readFirstN(int64(qLength))
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if len(res) != qLength {
		t.Fatalf("wrong result length: %d; expected: %d", len(res), qLength)
	}
	if bytes.Compare(res, b[:qLength]) != 0 {
		t.Fatalf("expected res be equivalent to %s; got: %s", string(b[:qLength]), string(res))
	}
	b2, err := ioutil.ReadAll(rc)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if bytes.Compare(b, b2) != 0 {
		t.Fatalf("expected b be equivalent to b2")
	}
}
