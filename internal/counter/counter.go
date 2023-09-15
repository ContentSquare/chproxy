package counter

import "sync/atomic"

type Counter struct {
	value atomic.Uint32
}

func (c *Counter) Store(n uint32) { c.value.Store(n) }

func (c *Counter) Load() uint32 { return c.value.Load() }

func (c *Counter) Dec() { c.value.Add(^uint32(0)) }

func (c *Counter) Inc() uint32 { return c.value.Add(1) }
