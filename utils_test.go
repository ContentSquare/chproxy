package main

import (
	"bytes"
	"net/http"
	"net/url"
	"testing"
)

func TestFetchQuery(t *testing.T) {
	testCases := []struct {
		name     string
		req      *http.Request
		expected string
	}{
		{
			name:     "get param",
			req:      reqWithGetParam(),
			expected: "SELECT column FROM table",
		},
		{
			name:     "post param",
			req:      reqWithPostParam(),
			expected: "SELECT column FROM table",
		},
		{
			name:     "combined params",
			req:      reqCombined(),
			expected: "SELECT column FROM table",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			query := getQuery(tc.req)
			if string(query) != tc.expected {
				t.Errorf("got: %q; expected: %q", string(query), tc.expected)
			}
		})
	}
}

func reqWithGetParam() *http.Request {
	req, _ := http.NewRequest("GET", "", nil)
	params := make(url.Values)
	params.Set("query", "SELECT column FROM table")
	req.URL.RawQuery = params.Encode()
	return req
}

func reqWithPostParam() *http.Request {
	body := bytes.NewBufferString("SELECT column FROM table")
	req, _ := http.NewRequest("POST", "", body)
	return req
}

func reqCombined() *http.Request {
	body := bytes.NewBufferString("FROM table")
	req, _ := http.NewRequest("POST", "", body)
	params := make(url.Values)
	params.Set("query", "SELECT column")
	req.URL.RawQuery = params.Encode()
	return req
}
