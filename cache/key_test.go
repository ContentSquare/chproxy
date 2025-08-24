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
			expected: "4e3e71f4d94f34b6c8cab4888486a116",
		},
		{
			key: &Key{
				Query:          []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				AcceptEncoding: "gzip",
				Version:        2,
			},
			expected: "ba19aeff43f8cd4440a28f883201f342",
		},
		{
			key: &Key{
				Query:          []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				AcceptEncoding: "gzip",
				DefaultFormat:  "JSON",
				Version:        2,
			},
			expected: "341b43e5ce0ceafb3f49664d9b124618",
		},
		{
			key: &Key{
				Query:          []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				AcceptEncoding: "gzip",
				DefaultFormat:  "JSON",
				Database:       "foobar",
				Version:        2,
			},
			expected: "bd864dd1d3dfec711f15830a0601a11e",
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
			expected: "04e932bafebeeb6b4c7c07288b37db3c",
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
			expected: "2cd1c2db405f570c2eb61fd4a02b8f05",
		},
		{
			key: &Key{
				Query:           []byte("SELECT * FROM {table_name:Identifier} LIMIT 10"),
				QueryParamsHash: 3825709,
				Version:         3,
			},
			expected: "b39b64fc48f705017a4734e824c4e62a",
		},
		{
			key: &Key{
				Query:           []byte("SELECT * FROM {table_name:Identifier} LIMIT 10"),
				QueryParamsHash: 3825710,
				Version:         3,
			},
			expected: "925e7c71544fc398a97d8e49426d61e3",
		},
		{
			key: &Key{
				Query:              []byte("SELECT * FROM {table_name:Identifier} LIMIT 10"),
				QueryParamsHash:    3825710,
				Version:            3,
				UserCredentialHash: 234324,
			},
			expected: "12b86ad023144c27676195e0ebce7f22",
		},
		{
			key: &Key{
				Query:                 []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				AcceptEncoding:        "gzip",
				ClientProtocolVersion: "54460",
				DefaultFormat:         "JSON",
				Database:              "foobar",
				Version:               5,
			},
			expected: "63faa92cb204a36ded33036ca4d429d5",
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
