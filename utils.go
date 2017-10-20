package main

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"
	"time"
	"unsafe"

	"github.com/Vertamedia/chproxy/cache"
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

func unsafeStr2Bytes(s string) []byte {
	sh := (*reflect.StringHeader)(unsafe.Pointer(&s))
	bh := reflect.SliceHeader{
		Data: sh.Data,
		Len:  sh.Len,
		Cap:  sh.Len,
	}
	return *(*[]byte)(unsafe.Pointer(&bh))
}

func hashReq(req *http.Request) (string, error) {
	var err error
	var b []byte
	if req.Body != nil {
		b, err = ioutil.ReadAll(req.Body)
		if err != nil {
			return "", fmt.Errorf("error while reading body: %v", err)
		}
	}
	key := cache.GenerateKey(unsafeStr2Bytes(req.RequestURI), b)
	req.Body = ioutil.NopCloser(bytes.NewBuffer(b))
	return key, nil
}
