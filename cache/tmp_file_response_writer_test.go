package cache

import (
	"bytes"
	"log"
	"testing"
)

func Test_readHeader(t *testing.T) {
	val := "\x00\x00\x00(text/tab-separated-values; charset=UTF-8\x00\x00\x00\x000\n1\n2\n3\n4\n5\n"

	head, err := readHeader(bytes.NewBuffer([]byte(val)))
	if err!= nil {
		log.Fatalf("There is an issue man: %s", head)
	}

	log.Printf("Header: %s", head)
}