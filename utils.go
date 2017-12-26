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

	"github.com/Vertamedia/chproxy/chdecompressor"
	"github.com/Vertamedia/chproxy/log"
)

func respondWith(rw http.ResponseWriter, err error, status int) {
	log.ErrorWithCallDepth(err, 1)
	rw.WriteHeader(status)
	fmt.Fprintf(rw, "%s\n", err)
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
		pass := params.Get("password")
		return name, pass
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

// getQuerySnippet returns query snippet.
//
// getQuerySnippet must be called only for error reporting.
func getQuerySnippet(req *http.Request) string {
	if req.Method == http.MethodGet {
		return req.URL.Query().Get("query")
	}

	crc, ok := req.Body.(*cachedReadCloser)
	if !ok {
		crc = &cachedReadCloser{
			ReadCloser: req.Body,
		}
	}

	// 'read' request body, so it traps into to crc.
	// Ignore any errors, since getQuerySnippet is called only
	// during error reporting.
	io.Copy(ioutil.Discard, crc)
	data := crc.String()

	u := getDecompressor(req)
	if u == nil {
		return data
	}
	bs := bytes.NewBufferString(data)
	b, err := u.decompress(bs)
	if err == nil {
		return string(b)
	}
	// It is better to return partially decompressed data instead of an empty string.
	if len(b) > 0 {
		return string(b)
	}
	// The data failed to be decompressed. Return compressed data
	// instead of an empty string.
	return data
}

// getFullQuery returns full query from req.
func getFullQuery(req *http.Request) ([]byte, error) {
	if req.Method == http.MethodGet {
		return []byte(req.URL.Query().Get("query")), nil
	}
	data, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u := getDecompressor(req)
	if u == nil {
		return data, nil
	}
	br := bytes.NewReader(data)
	b, err := u.decompress(br)
	if err != nil {
		return nil, fmt.Errorf("cannot uncompress query: %s", err)
	}
	return b, nil
}

// canCacheQuery returns true if q can be cached.
func canCacheQuery(q []byte) bool {
	q = skipLeadingComments(q)

	// Currently only SELECT queries may be cached.
	if len(q) < len("SELECT") {
		return false
	}
	q = bytes.ToUpper(q[:len("SELECT")])
	return bytes.HasPrefix(q, []byte("SELECT"))
}

func skipLeadingComments(q []byte) []byte {
	for len(q) > 0 {
		switch q[0] {
		case '\t', '\n', '\v', '\f', '\r', ' ':
			q = q[1:]
		case '-':
			if len(q) < 2 || q[1] != '-' {
				return q
			}

			// skip `-- comment`
			n := bytes.IndexByte(q, '\n')
			if n < 0 {
				return nil
			}
			q = q[n+1:]
		case '/':
			if len(q) < 2 || q[1] != '*' {
				return q
			}

			// skip `/* comment */`
			for {
				n := bytes.IndexByte(q, '*')
				if n < 0 {
					return nil
				}
				q = q[n+1:]
				if len(q) == 0 {
					return nil
				}
				if q[0] == '/' {
					q = q[1:]
					break
				}
			}
		default:
			return q
		}
	}
	return nil
}

func getDecompressor(req *http.Request) decompressor {
	if req.Header.Get("Content-Encoding") == "gzip" {
		return gzipDecompressor{}
	}
	if req.URL.Query().Get("decompress") == "1" {
		return chDecompressor{}
	}
	return nil
}

type decompressor interface {
	decompress(r io.Reader) ([]byte, error)
}

type gzipDecompressor struct{}

func (dc gzipDecompressor) decompress(r io.Reader) ([]byte, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("cannot ungzip query: %s", err)
	}
	return ioutil.ReadAll(gr)
}

type chDecompressor struct{}

func (dc chDecompressor) decompress(r io.Reader) ([]byte, error) {
	lr := chdecompressor.NewReader(r)
	return ioutil.ReadAll(lr)
}
