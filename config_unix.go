package conntrack

// socketReusePort indicates that a given port is available to be bound by multiple discrete processes at the same time.
// While only one socket is active at any given time and the first socket to be bound must release in order to allow for
// traffic to proceed to the second socket, the bind operation will not fail.
const socketReusePort = 15
