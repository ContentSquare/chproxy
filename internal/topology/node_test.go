package topology

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/contentsquare/chproxy/internal/heartbeat"
	"github.com/stretchr/testify/assert"
)

var _ heartbeat.HeartBeat = &mockHeartbeat{}

type mockHeartbeat struct {
	interval time.Duration
	err      error
}

func (hb *mockHeartbeat) Interval() time.Duration {
	return hb.interval
}

func (hb *mockHeartbeat) IsHealthy(ctx context.Context, addr string) error {
	return hb.err
}

func TestPenalize(t *testing.T) {
	node := NewNode(&url.URL{Host: "127.0.0.1"}, nil, "test", "test")
	expectedLoad := uint32(0)
	assert.Equal(t, expectedLoad, node.CurrentLoad(), "got running queries %d; expected %d", node.CurrentLoad(), expectedLoad)

	node.Penalize()
	expectedLoad = uint32(DefaultPenaltySize)
	assert.Equal(t, expectedLoad, node.CurrentLoad(), "got running queries %d; expected %d", node.CurrentLoad(), expectedLoad)

	// do more penalties than `penaltyMaxSize` allows
	max := int(DefaultMaxSize/DefaultPenaltySize) * 2
	for i := 0; i < max; i++ {
		node.Penalize()
	}

	expectedLoad = uint32(DefaultMaxSize)
	assert.Equal(t, expectedLoad, node.CurrentLoad(), "got running queries %d; expected %d", node.CurrentLoad(), expectedLoad)

	// Still allow connections to increase.
	node.IncrementConnections()
	expectedLoad++
	assert.Equal(t, expectedLoad, node.CurrentLoad(), "got running queries %d; expected %d", node.CurrentLoad(), expectedLoad)
}

func TestStartHeartbeat(t *testing.T) {
	hb := &mockHeartbeat{
		interval: 10 * time.Millisecond,
		err:      nil,
	}

	done := make(chan struct{})
	defer close(done)

	node := NewNode(&url.URL{Host: "127.0.0.1"}, hb, "test", "test")

	// Node is eventually active after start.
	go node.StartHeartbeat(done)

	assert.Eventually(t, func() bool {
		return node.IsActive()
	}, time.Second, 100*time.Millisecond)

	// change heartbeat to error, node eventually becomes inactive.
	hb.err = errors.New("failed connection")

	assert.Eventually(t, func() bool {
		return !node.IsActive()
	}, time.Second, 100*time.Millisecond)

	// If error is removed node becomes active again.
	hb.err = nil

	assert.Eventually(t, func() bool {
		return node.IsActive()
	}, time.Second, 100*time.Millisecond)
}
