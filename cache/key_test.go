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
			expected: "ef4c039ea06ce6fd95f4ffef551ba029",
		},
		{
			key: &Key{
				Query:          []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				AcceptEncoding: "gzip",
				Version:        2,
			},
			expected: "cb83c486eea079a87a6e567ba9869111",
		},
		{
			key: &Key{
				Query:          []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				AcceptEncoding: "gzip",
				DefaultFormat:  "JSON",
				Version:        2,
			},
			expected: "89edc4ac678557d80063d1060b712808",
		},
		{
			key: &Key{
				Query:          []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				AcceptEncoding: "gzip",
				DefaultFormat:  "JSON",
				Database:       "foobar",
				Version:        2,
			},
			expected: "120d73469183ace3a31c941cfcc8dc13",
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
			expected: "8441149c2cba1503e201aa94cda949f7",
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
			expected: "882a1cfc54f86e75a3ee89757bd33672",
		},
		{
			key: &Key{
				Query:           []byte("SELECT * FROM {table_name:Identifier} LIMIT 10"),
				QueryParamsHash: 3825709,
				Version:         3,
			},
			expected: "9d7a76630ca453d120a7349c4b6fa23d",
		},
		{
			key: &Key{
				Query:           []byte("SELECT * FROM {table_name:Identifier} LIMIT 10"),
				QueryParamsHash: 3825710,
				Version:         3,
			},
			expected: "1899cf94d4c5a3dda9575df7d8734e9b",
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
