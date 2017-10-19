package cache

import (
	"testing"
)

func BenchmarkGenerateKey(b *testing.B) {
	v1 := []byte("http://localhost:8123/?")
	v2 := []byte("SELECT 1 FORMAT Pretty")
	for n := 0; n < b.N; n++ {
		GenerateKey(v1, v2)
	}
}

func BenchmarkGenerateKey_Parallel(b *testing.B) {
	v1 := []byte("http://localhost:8123/?")
	v2 := []byte("SELECT 1 FORMAT Pretty")
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			GenerateKey(v1, v2)
		}
	})
}
