package main

import (
	"context"
	"fmt"
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
