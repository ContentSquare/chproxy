package cache

import (
	"crypto/sha1"
	"fmt"
	"github.com/valyala/bytebufferpool"
	"math/rand"
	"time"
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

const charset = "abcdefghijklmnopqrstuvwxyz" +
	"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

var seededRand *rand.Rand = rand.New(
	rand.NewSource(time.Now().UnixNano()))

func randomString(length int) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}
