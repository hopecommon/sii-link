package stack

import (
	"context"
	"net"

	"github.com/hopecommon/sii-link/client"
	"github.com/hopecommon/sii-link/internal/ippool"
	"github.com/hopecommon/sii-link/internal/zcdns"
)

type Stack interface {
	Run()
	SetupResolve(r zcdns.LocalServer)
	SetupIPPool(ipPool *ippool.IPPool[client.DomainResource])
	DialTCP(ctx context.Context, addr *net.TCPAddr) (net.Conn, error)
	DialUDP(ctx context.Context, addr *net.UDPAddr) (net.Conn, error)
}
