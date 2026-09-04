package netdial

import (
	"context"
	"net"
)

// Dialer is the small network boundary used for outbound connections to
// remote hosts. Implementations may use the host network or an embedded
// userspace network such as Tailscale's tsnet.
type Dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type Direct struct{}

func (Direct) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, address)
}
