package conntrack

import (
	"errors"
	"io"
	"net"
)

type ListenCloser interface {
	Listen()
	io.Closer
}

type Monitor interface {
	ConnectionEstablished(net.Conn)
	ConnectionRefused(net.Conn, error)
	ConnectionClosed(net.Conn)
}

type Logger interface {
	Printf(string, ...any)
}

var (
	ErrShuttingDown       = errors.New("shutting down; no new connections are being accepted")
	ErrTooManyConnections = errors.New("too many active connections")
)
