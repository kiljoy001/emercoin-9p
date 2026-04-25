package emercoin9p

import (
	"bufio"
	"errors"
	"log"
	"net"
	"sync"

	"github.com/knusbaum/go9p"
)

type serverInfo struct {
	mu   sync.RWMutex
	addr string
	port int
}

func newServerInfo() *serverInfo {
	return &serverInfo{}
}

func (s *serverInfo) SetListener(addr net.Addr) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.addr = addr.String()
	if tcpAddr, ok := addr.(*net.TCPAddr); ok {
		s.port = tcpAddr.Port
	}
}

func (s *serverInfo) SetPort(port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.port = port
}

func (s *serverInfo) Port() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.port
}

func (s *serverInfo) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.addr
}

// RunningServer is a live TCP 9p server bound to a listener.
type RunningServer struct {
	namespace *Namespace
	listener  net.Listener
	done      chan error
	closeOnce sync.Once
}

// Listen starts serving the namespace on the supplied TCP address.
func Listen(ns *Namespace, addr string) (*RunningServer, error) {
	if addr == "" {
		addr = "127.0.0.1:0"
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	if ns.config.serverInfo != nil {
		ns.config.serverInfo.SetListener(listener.Addr())
	}

	running := &RunningServer{
		namespace: ns,
		listener:  listener,
		done:      make(chan error, 1),
	}

	go running.serve()

	return running, nil
}

// ListenRandom starts serving the namespace on a random loopback port.
func ListenRandom(ns *Namespace) (*RunningServer, error) {
	return Listen(ns, "127.0.0.1:0")
}

func (s *RunningServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				s.done <- nil
			} else {
				s.done <- err
			}
			close(s.done)
			return
		}

		go func(nc net.Conn) {
			defer nc.Close()
			read := bufio.NewReader(nc)
			if err := go9p.ServeReadWriter(read, nc, s.namespace.Server()); err != nil && !errors.Is(err, net.ErrClosed) {
				log.Printf("emercoin9p connection error: %v", err)
			}
		}(conn)
	}
}

// Addr returns the bound listener address.
func (s *RunningServer) Addr() string {
	return s.listener.Addr().String()
}

// Port returns the bound TCP port.
func (s *RunningServer) Port() int {
	if tcpAddr, ok := s.listener.Addr().(*net.TCPAddr); ok {
		return tcpAddr.Port
	}
	return 0
}

// Close stops accepting new connections.
func (s *RunningServer) Close() error {
	var err error
	s.closeOnce.Do(func() {
		err = s.listener.Close()
	})
	return err
}

// Wait blocks until the listener accept loop exits.
func (s *RunningServer) Wait() error {
	return <-s.done
}
