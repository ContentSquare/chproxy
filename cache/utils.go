package cache

import (
	"crypto/sha1"
	"fmt"
	"github.com/valyala/bytebufferpool"
	"reflect"
	"unsafe"
)

var hashPool bytebufferpool.Pool

func GenerateKey(values ...[]byte) string {
	bb := hashPool.Get()
	for _, v := range values {
		bb.B = append(bb.B, v...)
	}
	key := fmt.Sprintf("%x", sha1.Sum(bb.B))
	hashPool.Put(bb)
	return key
}

func UnsafeStr2Bytes(s string) []byte {
	sh := (*reflect.StringHeader)(unsafe.Pointer(&s))
	bh := reflect.SliceHeader{
		Data: sh.Data,
		Len:  sh.Len,
		Cap:  sh.Len,
	}
	return *(*[]byte)(unsafe.Pointer(&bh))
}
