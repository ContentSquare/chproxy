package counter

import "sync/atomic"

type Counter struct {
	value uint32
}

func (c *Counter) Store(n uint32) { atomic.StoreUint32(&c.value, n) }

func (c *Counter) Load() uint32 { return atomic.LoadUint32(&c.value) }

func (c *Counter) Dec() { atomic.AddUint32(&c.value, ^uint32(0)) }

func (c *Counter) Inc() uint32 { return atomic.AddUint32(&c.value, 1) }
