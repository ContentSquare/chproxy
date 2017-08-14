package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
)

// We don't use query_id because for distributed processing, the query ID is not passed to remote servers
func killQuery(uname string, elapsed float64) error {
	q := fmt.Sprintf("KILL QUERY WHERE initial_user = '%s' AND elapsed >= %d", uname, int(elapsed))
	return postQuery(q)
}

func postQuery(q string) error {
	resp, err := http.Post(fmt.Sprintf("%s/?query=%s", *addr, url.QueryEscape(q)), "", nil)
	if err != nil {
		return fmt.Errorf("error in clickhouse query %q at %q: %s", q, *addr, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		responseBody, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code returned from query %q at %q: %d. Expecting %d. Response body: %q",
			q, *addr, resp.StatusCode, http.StatusOK, responseBody)
	}
	return nil
}
