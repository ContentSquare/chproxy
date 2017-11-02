package cache

import (
	"sync/atomic"
	"testing"
)

var Sink uint32

func BenchmarkKeyString(b *testing.B) {
	k := Key{
		Query:          []byte("SELECT 1 FROM system.numbers LIMIT 10"),
		AcceptEncoding: "gzip",
		DefaultFormat:  "JSON",
		Database:       "foobar",
	}
	b.RunParallel(func(pb *testing.PB) {
		n := 0
		for pb.Next() {
			s := k.String()
			n += len(s)
		}
		atomic.AddUint32(&Sink, uint32(n))
	})
}
