package log

import (
	"bytes"
	"log"
	"testing"

	"github.com/contentsquare/chproxy/config"
	"github.com/stretchr/testify/assert"
)

func TestLogMask(t *testing.T) {
	err := InitReplacer([]config.LogMask{
		{
			Regex:       `(s3\(\s*'(?:(?:\\'|[^'])*)'\s*,\s*'(?:(?:\\'|[^'])*)'\s*,\s*')((?:\\'|[^'])*)(')`,
			Replacement: "$1******$3",
		},
	})
	assert.NoError(t, err)
	var b bytes.Buffer
	testLogger := log.New(&b, "DEBUG: ", stdLogFlags)
	err = testLogger.Output(outputCallDepth,
		mask("select * from s3('http://s3server/bucket', 'access-key', 'seret-key', 'CSVWithNamesAndTypes')"))
	assert.NoError(t, err)
	res, err := b.ReadString('\n')
	assert.NoError(t, err)
	assert.Contains(t, res, "select * from s3('http://s3server/bucket', 'access-key', '******', 'CSVWithNamesAndTypes')")
}
