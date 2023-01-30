package conntrack

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

type server struct {
	ctx            context.Context
	shutdown       context.CancelFunc
	name           string
	network        string
	address        string
	newListener    func(context.Context, string, string) (net.Listener, error)
	newConnection  chan net.Conn
	maxConnections int
	delay          time.Duration
	waiter         sync.WaitGroup
	mutex          sync.Mutex
	active         map[net.Conn]struct{}
	monitor        Monitor
	logger         Logger
}

func New(options ...option) Server {
	config := newConfig(options)

	child, shutdown := context.WithCancel(config.Context)
	return &server{
		ctx:            child,
		shutdown:       shutdown,
		name:           config.Name,
		network:        config.Network,
		address:        config.Address,
		newListener:    config.NewListener,
		newConnection:  make(chan net.Conn, config.BufferSize),
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
	defer this.closeActiveConnections()

	if listener, err := this.newListener(this.ctx, this.network, this.address); err == nil {
		this.logger.Printf("[INFO] Listening for [%s] traffic [%s://%s]...", this.name, this.network, this.address)
		this.listen(listener)

	} else if errors.Is(err, context.Canceled) {
		return

	} else {
		this.logger.Printf("[WARN] Unable to initialize listening socket for [%s] at [%s://%s]: %s", this.name, this.network, this.address, err)
	}
}
func (this *server) listen(listener net.Listener) {
	go this.closeListener(listener)

	for this.isAlive() && this.acceptConnection(listener) {
	}
}
func (this *server) acceptConnection(listener net.Listener) bool {
	if connection, err := listener.Accept(); errors.Is(err, context.Canceled) {
		return false

	} else if err != nil {
		this.monitor.ConnectionRefused(connection, err)

	} else if err = this.handleConnection(connection); err != nil {
		_ = connection.Close()
		this.monitor.ConnectionRefused(connection, ErrShuttingDown)

	} else {
		this.logger.Printf("[DEBUG] Connection established with [%s://%s] for [%s].", this.network, connection.RemoteAddr(), this.name)
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

	this.newConnection <- newManagedConnection(connection, this.protectedCloseConnection)
	return nil
}
func (this *server) connectionAllowed(_ net.Conn) error {
	if !this.isAlive() {
		return ErrShuttingDown
	}

	if len(this.active) >= this.maxConnections {
		return ErrTooManyConnections
	}

	// FUTURE: allowed list of source IPs or subnets
	return nil
}
func (this *server) isAlive() bool {
	select {
	case <-this.ctx.Done():
		return false
	default:
		return true
	}
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

	this.mutex.Lock()
	defer this.mutex.Unlock()

	_ = listener.Close()
	this.logger.Printf("[INFO] Closed listener for [%s] traffic on [%s://%s].", this.name, this.network, this.address)
	close(this.newConnection)
}
func (this *server) closeActiveConnections() {
	this.mutex.Lock()
	defer this.mutex.Unlock()

	if len(this.active) == 0 {
		return
	}

	this.delayClosingActive()

	for connection := range this.active {
		_ = this.closeConnection(connection)
	}
}
func (this *server) protectedCloseConnection(connection net.Conn) error {
	this.mutex.Lock()
	defer this.mutex.Unlock()

	if _, found := this.active[connection]; found {
		return this.closeConnection(connection)
	}

	return nil // connection closed previously: via connection.Close() *or* the server has already been shut down.
}
func (this *server) closeConnection(connection net.Conn) error {
	delete(this.active, connection)
	err := connection.Close()
	this.logger.Printf("[DEBUG] Connection closed for [%s] with [%s://%s].", this.name, this.network, connection.RemoteAddr())
	this.monitor.ConnectionClosed(connection)
	this.waiter.Done()
	return err
}
func (this *server) delayClosingActive() {
	if this.delay == 0 {
		return
	}

	this.logger.Printf("[INFO] Waiting for [%s] before shutting down %d active connection(s) for [%s] at [%s://%s].", this.delay, len(this.active), this.name, this.network, this.address)
	ctx, cancel := context.WithTimeout(this.ctx, this.delay)
	defer cancel()
	<-ctx.Done()
}

func (this *server) ConnectionEstablished() <-chan net.Conn { return this.newConnection }

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type managedConnection struct {
	net.Conn
	closer func(net.Conn) error
}

func newManagedConnection(connection net.Conn, closer func(net.Conn) error) net.Conn {
	return &managedConnection{Conn: connection, closer: closer}
}

func (this *managedConnection) Close() error {
	return this.closer(this.Conn)
}
