package main

import (
	"github.com/hagen1778/chproxy/log"
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

// setAuth applies passed credentials as Basic Auth
// by rewriting possible previous Basic Auth and URL params
// which is enough for ClickHouse to check authorization
func setAuth(req *http.Request, user, password string) {
	req.SetBasicAuth(user, password)
	params := req.URL.Query()
	params.Del("user")
	params.Del("password")
	req.URL.RawQuery = params.Encode()
}
