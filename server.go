package conntrack

import (
	"context"
	"io"
	"net"
	"sync"
	"time"
)

type server struct {
	ctx            context.Context
	shutdown       context.CancelFunc
	address        string
	network        string
	newListener    func(context.Context, string, string) (net.Listener, error)
	newConnection  func(net.Conn)
	maxConnections int
	delay          time.Duration
	waiter         sync.WaitGroup
	mutex          sync.Mutex
	active         map[net.Conn]struct{}
	monitor        Monitor
	logger         Logger
}

func New(options ...option) ListenCloser {
	config := newConfig(options)

	child, shutdown := context.WithCancel(config.Context)
	return &server{
		ctx:            child,
		shutdown:       shutdown,
		address:        config.Address,
		network:        config.Network,
		newListener:    config.NewListener,
		newConnection:  config.NewConnection,
		maxConnections: config.MaxConnections,
		delay:          config.ShutdownDelay,
		waiter:         sync.WaitGroup{},
		mutex:          sync.Mutex{},
		active:         make(map[net.Conn]struct{}, 16),
		logger:         config.Logger,
		monitor:        config.Monitor,
	}
}

func (this *server) Listen() {
	defer this.awaitCleanShutdown()

	if listener, err := this.newListener(this.ctx, this.network, this.address); err == nil {
		this.logger.Printf("[INFO] Listening for [%s] traffic on [%s]...", this.network, this.address)
		this.listen(listener)

	} else if err == context.Canceled {
		return

	} else {
		this.logger.Printf("[WARN] Unable to initialize listening socket: %s", err)
	}

	this.closeActive()
}
func (this *server) listen(listener net.Listener) {
	go this.closeListener(listener)

	for this.acceptConnection(listener) {
	}
}
func (this *server) acceptConnection(listener net.Listener) bool {
	if connection, err := listener.Accept(); err == context.Canceled {
		return false

	} else if err != nil {
		this.logger.Printf("[WARN] Unable to accept connection: %s", err)
		this.monitor.ConnectionRefused(connection, err)

	} else if err = this.handleConnection(connection); err != nil {
		_ = connection.Close()
		this.monitor.ConnectionRefused(connection, ErrShuttingDown)

	} else {
		this.logger.Printf("[DEBUG] Connection with [%s] established.", connection.RemoteAddr())
		this.monitor.ConnectionEstablished(connection)
	}

	return true
}
func (this *server) handleConnection(connection net.Conn) error {
	this.mutex.Lock()
	this.mutex.Unlock()

	if err := this.connectionAllowed(connection); err != nil {
		return err
	}

	this.waiter.Add(1)
	this.active[connection] = struct{}{}

	managed := newManagedConnection(connection, this.cleanupConnection)
	go this.newConnection(managed)

	return nil
}

func (this *server) connectionAllowed(_ net.Conn) error {
	select {
	case <-this.ctx.Done():
		return ErrShuttingDown
	default:
		// running
	}

	if len(this.active) >= this.maxConnections {
		return ErrTooManyConnections
	}

	// FUTURE: allowed list of source IPs or subnets
	return nil
}

func (this *server) Close() error {
	this.shutdown()
	return nil
}
func (this *server) awaitCleanShutdown() {
	<-this.ctx.Done()  // blocks until context is canceled via parent or caller invoking Close() directly
	this.waiter.Wait() // ensure any active connections are closed
}
func (this *server) closeListener(listener io.Closer) {
	<-this.ctx.Done() // blocks until context is canceled via parent or caller invoking Close() directly
	_ = listener.Close()
	this.logger.Printf("[INFO] Listener for [%s] traffic on [%s] closed.", this.network, this.address)
}
func (this *server) closeActive() {
	this.mutex.Lock()
	defer this.mutex.Unlock()

	if len(this.active) == 0 {
		return
	}

	this.delayClosingActive()

	for connection := range this.active {
		delete(this.active, connection)
		_ = connection.Close()
		this.waiter.Done()

		this.logger.Printf("[DEBUG] Connection with [%s] closed.", connection.RemoteAddr())
		this.monitor.ConnectionClosed(connection)
	}
}
func (this *server) cleanupConnection(connection net.Conn) {
	this.mutex.Lock()
	defer this.mutex.Unlock()

	if _, found := this.active[connection]; !found {
		return // connection closed previously: via connection.Close() *or* the server has already been shut down.
	}

	defer this.waiter.Done()
	delete(this.active, connection)
	this.logger.Printf("[DEBUG] Closed with [%s] closed.", connection.RemoteAddr())
	this.monitor.ConnectionClosed(connection)
}

func (this *server) delayClosingActive() {
	if this.delay == 0 {
		return
	}

	this.logger.Printf("[INFO] Waiting for [%s] before shutting down %d active connections.", len(this.active), this.delay)
	ctx, cancel := context.WithTimeout(this.ctx, this.delay)
	defer cancel()
	<-ctx.Done()
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type managedConnection struct {
	net.Conn
	cleanup func(net.Conn)
}

func newManagedConnection(connection net.Conn, cleanup func(net.Conn)) net.Conn {
	return &managedConnection{Conn: connection, cleanup: cleanup}
}

func (this *managedConnection) Close() error {
	err := this.Conn.Close()
	this.cleanup(this)
	return err
}
