package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

func TestCanCacheQuery(t *testing.T) {
	testCanCacheQuery(t, "", false)
	testCanCacheQuery(t, "   ", false)
	testCanCacheQuery(t, "INSERT aaa", false)
	testCanCacheQuery(t, "\t  INSERT aaa   ", false)
	testCanCacheQuery(t, "select", true)
	testCanCacheQuery(t, "\t\t   SELECT 123   ", true)
	testCanCacheQuery(t, "\t\t   sElECt 123   ", true)
}

func testCanCacheQuery(t *testing.T, q string, expected bool) {
	t.Helper()
	canCache := canCacheQuery([]byte(q))
	if canCache != expected {
		t.Fatalf("unexpected result %v; expecting %v", canCache, expected)
	}
}

func TestGetQuerySnippetGET(t *testing.T) {
	req, _ := http.NewRequest("GET", "", nil)
	params := make(url.Values)
	q := "SELECT column FROM table"
	params.Set("query", q)
	req.URL.RawQuery = params.Encode()
	query := getQuerySnippet(req)
	if query != q {
		t.Fatalf("got: %q; expected: %q", query, q)
	}
}

func TestGetQuerySnippetPOST(t *testing.T) {
	q := "SELECT column FROM table"
	body := bytes.NewBufferString(q)
	req, err := http.NewRequest("POST", "", body)
	if err != nil {
		panic(fmt.Sprintf("BUG: unexpected error: %s", err))
	}
	query := getQuerySnippet(req)
	if query != q {
		t.Fatalf("got: %q; expected: %q", query, q)
	}
}

func TestGetQuerySnippetGzipped(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	q := makeQuery(1000)
	_, err := zw.Write([]byte(q))
	if err != nil {
		t.Fatal(err)
	}
	zw.Close()
	req, err := http.NewRequest("POST", "http://127.0.0.1:9090", &buf)
	req.Header.Set("Content-Encoding", "gzip")
	if err != nil {
		t.Fatal(err)
	}
	query := getQuerySnippet(req)
	if query[:100] != string(q[:100]) {
		t.Fatalf("got: %q; expected: %q", query[:100], q[:100])
	}
}

func TestGetFullQueryGzipped(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	q := makeQuery(1000)
	_, err := zw.Write([]byte(q))
	if err != nil {
		t.Fatal(err)
	}
	zw.Close()
	req, err := http.NewRequest("POST", "http://127.0.0.1:9090", &buf)
	req.Header.Set("Content-Encoding", "gzip")
	if err != nil {
		t.Fatal(err)
	}
	query, err := getFullQuery(req)
	if err != nil {
		t.Fatal(err)
	}
	if string(query) != string(q) {
		t.Fatalf("got: %q; expected %q", query, q)
	}
}

func makeQuery(n int) []byte {
	q1 := "SELECT column "
	q2 := "WHERE Date=today()"

	var b []byte
	b = append(b, q1...)
	for i := 0; i < n; i++ {
		b = append(b, fmt.Sprintf("col%d, ", i)...)
	}
	b = append(b, q2...)
	return b
}
