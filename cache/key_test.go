package cache

import (
	"sync/atomic"
	"testing"
)

func TestKeyString(t *testing.T) {
	testCases := []struct {
		key      *Key
		expected string
	}{
		{
			key: &Key{
				Query:   []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				Version: 2,
			},
			expected: "bebe3382e36ffdeea479b45d827b208a",
		},
		{
			key: &Key{
				Query:          []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				AcceptEncoding: "gzip",
				Version:        2,
			},
			expected: "498c1af30fb94280fd7c7225c0c8fb39",
		},
		{
			key: &Key{
				Query:          []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				AcceptEncoding: "gzip",
				DefaultFormat:  "JSON",
				Version:        2,
			},
			expected: "720292aa0647cc5e53e0b6e6033eef34",
		},
		{
			key: &Key{
				Query:          []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				AcceptEncoding: "gzip",
				DefaultFormat:  "JSON",
				Database:       "foobar",
				Version:        2,
			},
			expected: "5c6a70736d71e570faca739c4557780c",
		},
		{
			key: &Key{
				Query:          []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				AcceptEncoding: "gzip",
				DefaultFormat:  "JSON",
				Database:       "foobar",
				Namespace:      "ns123",
				Version:        2,
			},
			expected: "08b4baf6825e53bbd18136a88abda4f8",
		},
		{
			key: &Key{
				Query:          []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				AcceptEncoding: "gzip",
				DefaultFormat:  "JSON",
				Database:       "foobar",
				Compress:       "1",
				Namespace:      "ns123",
				Version:        2,
			},
			expected: "0e043f23ccd1b9039b33623b3b7c114a",
		},
	}

	for _, tc := range testCases {
		s := tc.key.String()
		if !cachefileRegexp.MatchString(s) {
			t.Fatalf("invalid key string format: %q", s)
		}
		if s != tc.expected {
			t.Fatalf("unexpected key string: %q; expecting: %q", s, tc.expected)
		}
	}
}

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
