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
			expected: "f11e8438adeeb325881c9a4da01925b3",
		},
		{
			key: &Key{
				Query:          []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				AcceptEncoding: "gzip",
				Version:        2,
			},
			expected: "045cbb29a40a81c42378569cf0bc4078",
		},
		{
			key: &Key{
				Query:          []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				AcceptEncoding: "gzip",
				DefaultFormat:  "JSON",
				Version:        2,
			},
			expected: "186386850c49c60a49dbf7af89c671c9",
		},
		{
			key: &Key{
				Query:          []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				AcceptEncoding: "gzip",
				DefaultFormat:  "JSON",
				Database:       "foobar",
				Version:        2,
			},
			expected: "68f3231d17cad0a3473e63f419e07580",
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
			expected: "8f5e765e69df7c24a58f13cdf752ad2f",
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
			expected: "93a121f03f438ef7969540c78e943e2c",
		},
		{
			key: &Key{
				Query:           []byte("SELECT * FROM {table_name:Identifier} LIMIT 10"),
				QueryParamsHash: 3825709,
				Version:         3,
			},
			expected: "7edddc7d9db4bc4036dee36893f57cb1",
		},
		{
			key: &Key{
				Query:           []byte("SELECT * FROM {table_name:Identifier} LIMIT 10"),
				QueryParamsHash: 3825710,
				Version:         3,
			},
			expected: "68ba76fb53a6fa71ba8fe63dd34a2201",
		},
		{
			key: &Key{
				Query:              []byte("SELECT * FROM {table_name:Identifier} LIMIT 10"),
				QueryParamsHash:    3825710,
				Version:            3,
				UserCredentialHash: 234324,
			},
			expected: "c5b58ecb4ff026e62ee846dc63c749d5",
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
