package cache

import "net/http"

type ResponseWriter interface {
	Write([]byte) (int, error)
	Rollback() error
	Commit() error
	Header() http.Header
}
