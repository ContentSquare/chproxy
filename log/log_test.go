package log

import (
	"bytes"
	"log"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogMask(t *testing.T) {
	InitReplacer([]string{"secret1", "secret2"})
	var b bytes.Buffer
	testLogger := log.New(&b, "DEBUG: ", stdLogFlags)
	err := testLogger.Output(outputCallDepth, mask("some message with secret1 and secret2")) // nolint
	assert.NoError(t, err)
	res, err := b.ReadString('\n')
	assert.NoError(t, err)
	assert.Contains(t, res, "some message with <xxx> and <xxx>")
}
