package main

import (
	"bytes"
	"io/ioutil"
	"net/http/httptest"
	"testing"
)

func TestReadFirstN(t *testing.T) {
	b := makeQuery(100)
	rc := &statReadCloser{
		ReadCloser:       ioutil.NopCloser(bytes.NewReader(b)),
		requestBodyBytes: badRequest,
	}
	var qLength = 10
	req := httptest.NewRequest("POST", "http://localhost", rc)
	res := fetchQuery(req, int64(qLength))
	if len(res) != qLength {
		t.Fatalf("wrong result length: %d; expected: %d", len(res), qLength)
	}
	if bytes.Compare(res, b[:qLength]) != 0 {
		t.Fatalf("expected res be equivalent to %s; got: %s", string(b[:qLength]), string(res))
	}
}
