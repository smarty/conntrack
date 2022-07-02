package conntrack

import (
	"context"
	"net"
	"syscall"
	"time"
)

// FUTURE:
// notify active connections to give them a few moments before being closed
// allowed list of source IPs

type configuration struct {
	Context        context.Context
	Name           string
	Network        string
	Address        string
	NewListener    func(context.Context, string, string) (net.Listener, error)
	BufferSize     int
	MaxConnections int
	ShutdownDelay  time.Duration
	Monitor        Monitor
	Logger         Logger
}

func newConfig(options []option) configuration {
	this := configuration{}
	Options.apply(options...)(&this)
	return this
}

func (singleton) Context(value context.Context) option {
	return func(this *configuration) { this.Context = value }
}
func (singleton) Name(value string) option {
	return func(this *configuration) { this.Name = value }
}
func (singleton) Network(value string) option {
	return func(this *configuration) { this.Network = value }
}
func (singleton) Address(value string) option {
	return func(this *configuration) { this.Address = value }
}
func (singleton) NewListener(value func(context.Context, string, string) (net.Listener, error)) option {
	return func(this *configuration) { this.NewListener = value }
}
func (singleton) BufferSize(value uint16) option {
	return func(this *configuration) { this.BufferSize = int(value) }
}
func (singleton) MaxConnections(value uint32) option {
	return func(this *configuration) { this.MaxConnections = int(value) }
}
func (singleton) ShutdownDelay(value time.Duration) option {
	if value < 0 {
		value = 0
	}

	return func(this *configuration) { this.ShutdownDelay = value }
}
func (singleton) Monitor(value Monitor) option {
	return func(this *configuration) { this.Monitor = value }
}
func (singleton) Logger(value Logger) option {
	return func(this *configuration) { this.Logger = value }
}

func (singleton) apply(options ...option) option {
	return func(this *configuration) {
		for _, item := range Options.defaults(options...) {
			item(this)
		}
	}
}
func (singleton) defaults(options ...option) []option {
	defaultListenConfig := &net.ListenConfig{
		Control: func(_, _ string, conn syscall.RawConn) error {
			return conn.Control(func(descriptor uintptr) {
				_ = syscall.SetsockoptInt(int(descriptor), syscall.SOL_SOCKET, socketReusePort, 1)
			})
		},
	}

	noop := &nop{}
	return append([]option{
		Options.Context(context.Background()),
		Options.Name("listener"),
		Options.Network("tcp"),
		Options.Address("127.0.0.1:9999"),
		Options.NewListener(defaultListenConfig.Listen),
		Options.BufferSize(32),
		Options.MaxConnections(1024),
		Options.ShutdownDelay(time.Millisecond * 100),
		Options.Monitor(noop),
		Options.Logger(noop),
	}, options...)
}

type singleton struct{}
type option func(*configuration)

var Options singleton

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type nop struct{}

func (*nop) ConnectionRefused(_ net.Conn, _ error) {}
func (*nop) ConnectionEstablished(_ net.Conn)      {}
func (*nop) ConnectionClosed(_ net.Conn)           {}
func (*nop) Printf(string, ...any)                 {}
