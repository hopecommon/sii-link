package atrust

import (
	"context"
	"net"
	"sync"
	"time"
)

type tcpTunnelDialFunc func(context.Context, string) (net.Conn, error)

type tcpTunnelNodePool struct {
	idle  chan net.Conn
	total int
	all   map[net.Conn]struct{}
}

// tcpTunnelPool keeps a bounded number of reusable TLS connections per relay.
// Individual aTrust flows still use the short-tunnel framing; only a cleanly
// closed transport is returned to the pool.
type tcpTunnelPool struct {
	size int
	dial tcpTunnelDialFunc

	mu     sync.Mutex
	closed bool
	nodes  map[string]*tcpTunnelNodePool
}

type tcpTunnelLease struct {
	pool    *tcpTunnelPool
	node    *tcpTunnelNodePool
	conn    net.Conn
	managed bool
	reused  bool
	once    sync.Once
}

func newTCPTunnelPool(size int, dial tcpTunnelDialFunc) *tcpTunnelPool {
	return &tcpTunnelPool{
		size:  size,
		dial:  dial,
		nodes: make(map[string]*tcpTunnelNodePool),
	}
}

func (p *tcpTunnelPool) nodeLocked(address string) *tcpTunnelNodePool {
	node := p.nodes[address]
	if node == nil {
		node = &tcpTunnelNodePool{
			idle: make(chan net.Conn, p.size),
			all:  make(map[net.Conn]struct{}),
		}
		p.nodes[address] = node
	}
	return node
}

func (p *tcpTunnelPool) Acquire(ctx context.Context, address string) (*tcpTunnelLease, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, net.ErrClosed
	}
	node := p.nodeLocked(address)
	select {
	case conn := <-node.idle:
		p.mu.Unlock()
		return &tcpTunnelLease{
			pool:    p,
			node:    node,
			conn:    conn,
			managed: true,
			reused:  true,
		}, nil
	default:
	}

	managed := node.total < p.size
	if managed {
		node.total++
	}
	p.mu.Unlock()

	conn, err := p.dial(ctx, address)
	if err != nil {
		if managed {
			p.mu.Lock()
			node.total--
			p.mu.Unlock()
		}
		return nil, err
	}
	if managed {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			_ = conn.Close()
			return nil, net.ErrClosed
		}
		node.all[conn] = struct{}{}
		p.mu.Unlock()
	}
	return &tcpTunnelLease{
		pool:    p,
		node:    node,
		conn:    conn,
		managed: managed,
	}, nil
}

func (l *tcpTunnelLease) Release() {
	l.once.Do(func() {
		if !l.managed {
			_ = l.conn.Close()
			return
		}
		l.pool.release(l)
	})
}

func (l *tcpTunnelLease) Discard() {
	l.once.Do(func() {
		_ = l.conn.Close()
		if !l.managed {
			return
		}
		l.pool.discard(l)
	})
}

func (p *tcpTunnelPool) release(lease *tcpTunnelLease) {
	if err := lease.conn.SetDeadline(time.Time{}); err != nil {
		_ = lease.conn.Close()
		p.discard(lease)
		return
	}

	p.mu.Lock()
	if p.closed {
		if _, ok := lease.node.all[lease.conn]; ok {
			delete(lease.node.all, lease.conn)
			lease.node.total--
		}
		p.mu.Unlock()
		_ = lease.conn.Close()
		return
	}
	if _, ok := lease.node.all[lease.conn]; !ok {
		p.mu.Unlock()
		_ = lease.conn.Close()
		return
	}
	select {
	case lease.node.idle <- lease.conn:
		p.mu.Unlock()
	default:
		delete(lease.node.all, lease.conn)
		lease.node.total--
		p.mu.Unlock()
		_ = lease.conn.Close()
	}
}

func (p *tcpTunnelPool) discard(lease *tcpTunnelLease) {
	p.mu.Lock()
	if _, ok := lease.node.all[lease.conn]; ok {
		delete(lease.node.all, lease.conn)
		lease.node.total--
	}
	p.mu.Unlock()
}

func (p *tcpTunnelPool) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	var conns []net.Conn
	for _, node := range p.nodes {
		for conn := range node.all {
			conns = append(conns, conn)
		}
		node.all = make(map[net.Conn]struct{})
		node.total = 0
	}
	p.mu.Unlock()

	for _, conn := range conns {
		_ = conn.Close()
	}
}
