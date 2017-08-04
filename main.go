package main

import (
	"net/http/httputil"
	"net/url"
	"log"
	"net/http"
	"time"
	"context"
	"fmt"
	"io/ioutil"
	"flag"
)

var addr           = flag.String("h", "http://localhost:8123", "ClickHouse web-interface host:port address with scheme")

func main() {
	rpURL, err := url.Parse(*addr)
	if err != nil {
		log.Fatal(err)
	}

	proxy := proxyHandler{
		httputil.NewSingleHostReverseProxy(rpURL),
	}

	log.Fatal(http.ListenAndServe(":8080", proxy))
}


type proxyHandler struct {
	*httputil.ReverseProxy
}

func (ph proxyHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	startTime := time.Now()
	ctx := context.Background()
	deadline := time.Second*3
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	req = req.WithContext(ctx)

	uname := extractUserFromRequest(req)
	log.Println("user:", uname)

	result := make(chan struct{})
	go func(){
		ph.ReverseProxy.ServeHTTP(rw, req)
		close(result)
	}()

	<-result
	if ctx.Err() != nil {
		if err := killQuery(uname, deadline.Seconds()); err != nil {
			log.Println("Can't kill query:", err)
		}
		rw.Write([]byte(ctx.Err().Error()))
	} else {
		log.Println("Request took", time.Since(startTime))
	}
}

func extractUserFromRequest(r *http.Request) string {
	if uname, _, ok := r.BasicAuth(); ok {
		return uname
	}

	if uname := r.Form.Get("user"); uname != "" {
		return uname
	}

	return "default"
}
// We don't use query_id because for distributed processing, the query ID is not passed to remote servers
func killQuery(uname string, elapsed float64) error {
	q := fmt.Sprintf("KILL QUERY WHERE initial_user = '%s' AND elapsed >= %d", uname, int(elapsed))
	log.Println(q)
	return doQuery(q)
}


func doQuery(q string) error {
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
