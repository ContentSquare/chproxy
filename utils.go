package main

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/hagen1778/chproxy/config"
	"github.com/hagen1778/chproxy/log"
)

var client = &http.Client{
	Timeout: time.Second * 60,
}

func doQuery(query, addr string) error {
	resp, err := client.Post(fmt.Sprintf("%s/?query=%s", addr, url.QueryEscape(query)), "", nil)
	if err != nil {
		return fmt.Errorf("error while executing clickhouse query %q at %q: %s", query, addr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		responseBody, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code returned from query %q at %q: %d. Expecting %d. Response body: %q",
			query, addr, resp.StatusCode, http.StatusOK, responseBody)
	}
	return nil
}

func respondWithErr(rw http.ResponseWriter, err error) {
	log.Errorf("proxy failed: %s", err)
	rw.WriteHeader(http.StatusInternalServerError)
	rw.Write([]byte(err.Error()))
}

func basicAuth(req *http.Request) (string, string) {
	if name, pass, ok := req.BasicAuth(); ok {
		return name, pass
	}

	if name := req.URL.Query().Get("user"); name != "" {
		if pass := req.URL.Query().Get("password"); name != "" {
			return name, pass
		}
	}

	return "default", ""
}

func isAllowedAddr(addr string, allowedNetworks []*config.Network) bool {
	if len(allowedNetworks) == 0 {
		return true
	}

	h, _, err := net.SplitHostPort(addr)
	if err != nil {
		log.Errorf("BUG: unexpected error while parsing RemoteAddr: %s", err)
		return false
	}

	ip := net.ParseIP(h)
	if ip == nil {
		log.Errorf("BUG: unexpected error while parsing IP: %s", h)
		return false
	}

	for _, ipnet := range allowedNetworks {
		if ipnet.Contains(ip) {
			return true
		}
	}

	return false
}
