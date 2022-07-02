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

type Server interface {
	ConnectionEstablished() <-chan net.Conn
	ListenCloser
}

type Monitor interface {
	ConnectionEstablished(net.Conn)
	ConnectionRejected(net.Conn, error)
	ConnectionClosed(net.Conn)
}

type Logger interface {
	Printf(string, ...any)
}

var (
	ErrShuttingDown       = errors.New("shutting down; no new connections are being accepted")
	ErrTooManyConnections = errors.New("too many active connections")
)
