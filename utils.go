package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/Vertamedia/chproxy/log"
)

func respondWith(rw http.ResponseWriter, err error, status int) {
	log.Error(err.Error())
	rw.WriteHeader(status)
	fmt.Fprintf(rw, "%s", err)
}

// getAuth retrieves auth credentials from request
// according to CH documentation @see "http://clickhouse.readthedocs.io/en/latest/reference_en.html#HTTP interface"
func getAuth(req *http.Request) (string, string) {
	if name, pass, ok := req.BasicAuth(); ok {
		return name, pass
	}
	// if basicAuth is empty - check URL params `user` and `password`
	params := req.URL.Query()
	if name := params.Get("user"); name != "" {
		if pass := params.Get("password"); name != "" {
			return name, pass
		}
	}
	// if still no credentials - treat it as `default` user request
	return "default", ""
}

const (
	okResponse       = "Ok.\n"
	isHealthyTimeout = 3 * time.Second
)

func isHealthy(addr string) error {
	req, err := http.NewRequest("GET", addr, nil)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), isHealthyTimeout)
	defer cancel()
	req = req.WithContext(ctx)

	startTime := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot send request in %s: %s", time.Since(startTime), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("non-200 status code: %s", resp.Status)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("cannot read response in %s: %s", time.Since(startTime), err)
	}
	r := string(body)
	if r != okResponse {
		return fmt.Errorf("unexpected response: %s", r)
	}
	return nil
}

// getQuerySnippet returns query snippet.
//
// getQuerySnippet must be called only for error reporting.
func getQuerySnippet(req *http.Request) string {
	if req.Method == http.MethodGet {
		return req.URL.Query().Get("query")
	}

	crc, ok := req.Body.(*cachedReadCloser)
	if !ok {
		crc = &cachedReadCloser{
			ReadCloser: req.Body,
		}
	}

	// 'read' request body, so it traps into to crc.
	// Ignore any errors, since getQuerySnippet is called only
	// during error reporting.
	io.Copy(ioutil.Discard, crc)
	data := crc.String()

	if req.Header.Get("Content-Encoding") != "gzip" {
		return data
	}

	bs := bytes.NewBufferString(data)
	gr, err := gzip.NewReader(bs)
	if err != nil {
		// It is better to return `gzipped` data instead
		// of an empty string if the data cannot be ungzipped.
		return data
	}

	// Ignore errors while reading gzipped body because it's partial read
	// and no warranties that we will read enough data to unzip it.
	result, _ := ioutil.ReadAll(gr)
	return string(result)
}

// getFullQuery returns full query from req.
func getFullQuery(req *http.Request) ([]byte, error) {
	if req.Method == http.MethodGet {
		return []byte(req.URL.Query().Get("query")), nil
	}
	data, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	if req.Header.Get("Content-Encoding") != "gzip" {
		return data, nil
	}

	br := bytes.NewReader(data)
	gr, err := gzip.NewReader(br)
	if err != nil {
		return nil, fmt.Errorf("cannot ungzip query: %s", err)
	}

	result, err := ioutil.ReadAll(gr)
	if err != nil {
		return nil, fmt.Errorf("cannot ungzip query: %s", err)
	}
	return result, nil
}

// canCacheQuery returns true if q can be cached.
func canCacheQuery(q []byte) bool {
	// Currently only SELECT queries may be cached.
	q = bytes.TrimSpace(q)
	if len(q) < len("SELECT") {
		return false
	}
	q = bytes.ToUpper(q[:len("SELECT")])
	return bytes.HasPrefix(q, []byte("SELECT"))
}
