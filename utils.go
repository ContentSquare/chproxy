package main

import (
	"fmt"
	"github.com/Vertamedia/chproxy/log"
	"io/ioutil"
	"net/http"
)

func respondWithErr(rw http.ResponseWriter, err error) {
	log.Errorf("proxy failed: %s", err)
	rw.WriteHeader(http.StatusInternalServerError)
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

const okResponse = "Ok.\n"

func isHealthy(addr string) (bool, error) {
	resp, err := client.Get(addr)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("non-200 status code: %s", resp.Status)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	r := string(body)
	if r != okResponse {
		return false, fmt.Errorf("unexpected response: %s", r)
	}
	return true, nil
}
