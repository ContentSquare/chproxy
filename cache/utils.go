package cache

import (
	"crypto/sha1"
	"fmt"
	"github.com/valyala/bytebufferpool"
	"os"
	"path/filepath"
	"reflect"
	"unsafe"
)

func dirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			size += info.Size()
		}
		return err
	})
	return size, err
}

var hashPool bytebufferpool.Pool

func GenerateKey(uri string, body []byte) string {
	bb := hashPool.Get()
	bb.B = append(bb.B, body...)
	bb.B = append(bb.B, unsafeStr2Bytes(uri)...)
	key := fmt.Sprintf("%x", sha1.Sum(bb.B))
	hashPool.Put(bb)
	return key
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
