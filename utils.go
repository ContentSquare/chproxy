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
	rw.Write([]byte(err.Error()))
}

// getAuth retrieves auth credentials from request
// according to CH documentation @see "http://clickhouse.readthedocs.io/en/latest/reference_en.html#HTTP interface"
func getAuth(req *http.Request) (string, string) {
	if name, pass, ok := req.BasicAuth(); ok {
		return name, pass
	}
	// if basicAuth is empty - check URL params `user` and `password`
	if name := req.URL.Query().Get("user"); name != "" {
		if pass := req.URL.Query().Get("password"); name != "" {
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

// max bytes to read from requests body
const maxQueryLength = 900 //5 * 1024 // 5KB

func getQueryStart(req *http.Request) []byte {
	result := fetchQuery(req, maxQueryLength)
	if len(result) > maxQueryLength {
		result = result[:maxQueryLength]
	}
	if req.Header.Get("Content-Encoding") != "gzip" {
		return result
	}
	buf := bytes.NewBuffer(result)
	gr, err := gzip.NewReader(buf)
	if err != nil {
		log.Errorf("error while creating gzip reader: %s", err)
		return nil
	}
	// ignore errors while reading gzipped body
	// because it's partial read and no warranties that
	// we will read enough data to unzip it
	result, _ = ioutil.ReadAll(gr)
	return result
}

// fetchQuery fetches query from POST or GET request
// @see http://clickhouse.readthedocs.io/en/latest/reference_en.html#HTTP interface
func fetchQuery(req *http.Request, n int64) []byte {
	if req.Method == http.MethodGet {
		return []byte(req.URL.Query().Get("query"))
	}
	if req.Body == nil {
		return nil
	}
	src, ok := req.Body.(*cachedReadCloser)
	if ok {
		cached := src.readCached()
		if len(cached) > 0 {
			return cached
		}
	}
	var r io.Reader
	r = req.Body
	if n > 0 {
		r = io.LimitReader(req.Body, n)
	}
	result, err := ioutil.ReadAll(r)
	if err != nil {
		log.Errorf("error while reading body: %s", err)
		return nil
	}
	return result
}
