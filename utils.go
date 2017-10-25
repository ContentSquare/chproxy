package main

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"compress/gzip"
	"github.com/Vertamedia/chproxy/log"
	"io"
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
	isHealthyTimeout = time.Second
)

func isHealthy(addr string) error {
	req, err := http.NewRequest("GET", addr, nil)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), isHealthyTimeout)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("non-200 status code: %s", resp.Status)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	r := string(body)
	if r != okResponse {
		return fmt.Errorf("unexpected response: %s", r)
	}
	return nil
}

func getQuery(req *http.Request) []byte {
	return bytes.TrimSpace(fetchQuery(req))
}

// max bytes to read from requests body
const maxQueryLength = 5 * 1024 // 5KB

// fetchQuery fetches query from POST or GET request
// @see http://clickhouse.readthedocs.io/en/latest/reference_en.html#HTTP interface
func fetchQuery(req *http.Request) []byte {
	if req.Method == http.MethodGet {
		return []byte(req.URL.Query().Get("query"))
	}

	var result []byte
	if req.Body == nil {
		return result
	}
	rc := req.Body.(*readCloser)
	// read only first maxQueryLength chars to save memory
	result, err := rc.readFirstN(maxQueryLength)
	if err != nil {
		log.Errorf("error while reading body: %s", err)
		return nil
	}
	if req.Header.Get("Content-Encoding") != "gzip" {
		return result
	}
	buf := bytes.NewBuffer(result)
	r, err := gzip.NewReader(buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil
	}
	result, err = ioutil.ReadAll(r)
	if err != nil {
		log.Errorf("error while reading gzipped body: %s", err)
		return nil
	}
	return result
}
