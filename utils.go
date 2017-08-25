package main

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

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

func parseNetworks(networks []string) (map[string]struct{}, error) {
	if len(networks) == 0 {
		return nil, nil
	}

	ips := make(map[string]struct{})
	for _, network := range networks {
		if strings.Contains(network, `/`) {
			ip, ipnet, err := net.ParseCIDR(network)
			if err != nil {
				return nil, err
			}

			for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); inc(ip) {
				ips[ip.String()] = struct{}{}
			}
		} else {
			ip := net.ParseIP(network)
			if ip == nil {
				return nil, fmt.Errorf("invalid IP")
			}

			ips[ip.String()] = struct{}{}
		}
	}

	return ips, nil
}

func inc(ip net.IP) {
	for j := len(ip)-1; j>=0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}