package topology

import (
	"context"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/contentsquare/chproxy/internal/counter"
	"github.com/contentsquare/chproxy/internal/heartbeat"
	"github.com/contentsquare/chproxy/log"
)

const (
	// prevents excess goroutine creating while penalizing overloaded host
	DefaultPenaltySize     = 5
	DefaultMaxSize         = 300
	DefaultPenaltyDuration = time.Second * 10
)

type nodeOpts struct {
	defaultActive   bool
	penaltySize     uint32
	penaltyMaxSize  uint32
	penaltyDuration time.Duration
}

func defaultNodeOpts() nodeOpts {
	return nodeOpts{
		penaltySize:     DefaultPenaltySize,
		penaltyMaxSize:  DefaultMaxSize,
		penaltyDuration: DefaultPenaltyDuration,
	}
}

type NodeOption interface {
	apply(*nodeOpts)
}

type defaultActive struct {
	active bool
}

func (o defaultActive) apply(opts *nodeOpts) {
	opts.defaultActive = o.active
}

func WithDefaultActiveState(active bool) NodeOption {
	return defaultActive{
		active: active,
	}
}

type Node struct {
	// Node Address.
	addr *url.URL

	// Whether this node is alive.
	active atomic.Bool

	// Counter of currently running connections.
	connections counter.Counter

	// Counter of unsuccesfull request to decrease host priority.
	penalty atomic.Uint32

	// Heartbeat function
	hb heartbeat.HeartBeat

	// TODO These fields are only used for labels in prometheus. We should have a different way to pass the labels.
	// For metrics only
	clusterName string
	replicaName string

	// Additional configuration options
	opts nodeOpts
}

func NewNode(addr *url.URL, hb heartbeat.HeartBeat, clusterName, replicaName string, opts ...NodeOption) *Node {
	nodeOpts := defaultNodeOpts()

	for _, opt := range opts {
		opt.apply(&nodeOpts)
	}

	n := &Node{
		addr:        addr,
		hb:          hb,
		clusterName: clusterName,
		replicaName: replicaName,
		opts:        nodeOpts,
	}

	if n.opts.defaultActive {
		n.SetIsActive(true)
	}

	return n
}

func (n *Node) IsActive() bool {
	return n.active.Load()
}

func (n *Node) SetIsActive(active bool) {
	n.active.Store(active)
}

// StartHeartbeat runs the heartbeat healthcheck against the node
// until the done channel is closed.
// If the heartbeat fails, the active status of the node is changed.
func (n *Node) StartHeartbeat(done <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	for {
		n.heartbeat(ctx)
		select {
		case <-done:
			cancel()
			return
		case <-time.After(n.hb.Interval()):
		}
	}
}

func (n *Node) heartbeat(ctx context.Context) {
	if err := n.hb.IsHealthy(ctx, n.addr.String()); err == nil {
		n.active.Store(true)
		reportNodeHealthMetric(n.clusterName, n.replicaName, n.Host(), true)
	} else {
		log.Errorf("error while health-checking %q host: %s", n.Host(), err)
		n.active.Store(false)
		reportNodeHealthMetric(n.clusterName, n.replicaName, n.Host(), false)
	}
}

// Penalize a node if a request failed to decrease it's priority.
// If the penalty is already at the maximum allowed size this function
// will not penalize the node further.
// A function will be registered to run after the penalty duration to
// increase the priority again.
func (n *Node) Penalize() {
	penalty := n.penalty.Load()
	if penalty >= n.opts.penaltyMaxSize {
		return
	}

	incrementPenaltiesMetric(n.clusterName, n.replicaName, n.Host())

	n.penalty.Add(n.opts.penaltySize)

	time.AfterFunc(n.opts.penaltyDuration, func() {
		n.penalty.Add(^uint32(n.opts.penaltySize - 1))
	})
}

// CurrentLoad returns the current node returns the number of open connections
// plus the penalty.
func (n *Node) CurrentLoad() uint32 {
	c := n.connections.Load()
	p := n.penalty.Load()
	return c + p
}

func (n *Node) CurrentConnections() uint32 {
	return n.connections.Load()
}

func (n *Node) CurrentPenalty() uint32 {
	return n.penalty.Load()
}

func (n *Node) IncrementConnections() {
	n.connections.Inc()
}

func (n *Node) DecrementConnections() {
	n.connections.Dec()
}

func (n *Node) Scheme() string {
	return n.addr.Scheme
}

func (n *Node) Host() string {
	return n.addr.Host
}

func (n *Node) ReplicaName() string {
	return n.replicaName
}

func (n *Node) String() string {
	return n.addr.String()
}
