package loadbalancer

import (
	"time"
)

// -----------------------------------------------------------------------------

// Server represents an upstream server in a load balancer.
type Server struct {
	lb          *LoadBalancer // NOTE: Go's Mark & Sweep plays well with this circular reference
	opts        ServerOptions
	index       int
	isDown      bool
	failCounter int
	// NOTE: failTimestamp has two uses:
	//       1. Marks the timestamp of the first access failure
	//       2. Marks the timestamp to put it again online when down
	failTimestamp time.Time
	userData      interface{}
}

// ServerGroup is a group of servers. Used to classify and track primary and backup servers.
type ServerGroup struct {
	srvList          []Server
	currServerIdx    int
	currServerWeight int
}

// ServerOptions specifies details about a server.
type ServerOptions struct {
	Weight      int
	MaxFails    int
	FailTimeout time.Duration
	IsBackup    bool
}

// -----------------------------------------------------------------------------

// UserData returns the server user data
func (srv *Server) UserData() interface{} {
	return srv.userData
}

// SetOnline marks a server as available
func (srv *Server) SetOnline() {
	// We only can change the online/offline status on primary servers
	if srv.opts.MaxFails == 0 || srv.opts.IsBackup {
		return
	}

	notifyUp := false

	// Lock access
	srv.lb.mtx.Lock()

	// Reset the failure counter
	srv.failCounter = 0

	// If the server was marked as down, put it online again
	if srv.isDown {
		srv.isDown = false
		srv.lb.primaryOnlineCount += 1

		notifyUp = true
	}

	// Unlock access
	srv.lb.mtx.Unlock()

	// Call event callback
	if notifyUp {
		srv.lb.raiseEvent(ServerUpEvent, srv)
	}
}

// SetOffline marks a server as unavailable
func (srv *Server) SetOffline() {
	// We only can change the online/offline status on primary servers
	if srv.opts.MaxFails == 0 || srv.opts.IsBackup {
		return
	}

	notifyDown := false

	// Lock access
	srv.lb.mtx.Lock()

	// If server is up
	if !srv.isDown && srv.failCounter < srv.opts.MaxFails {
		now := time.Now()

		// Increment the failure counter
		srv.failCounter += 1

		if srv.failCounter == 1 {
			// If it is the first failure, set the fail timestamp limit
			srv.failTimestamp = now.Add(srv.opts.FailTimeout)

		} else {

			// If this failure passed after the fail timeout, reset the counter
			if now.After(srv.failTimestamp) {
				srv.failCounter = 1
			}
		}

		// If we reach to the maximum failure count, put this server offline
		if srv.failCounter == srv.opts.MaxFails {
			srv.isDown = true
			srv.failTimestamp = now.Add(srv.opts.FailTimeout)
			srv.lb.primaryOnlineCount -= 1

			notifyDown = true
		}
	}

	// Unlock access
	srv.lb.mtx.Unlock()

	// Call event callback
	if notifyDown {
		srv.lb.raiseEvent(ServerDownEvent, srv)
	}
}
