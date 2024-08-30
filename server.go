package goredisclone

import (
	"fmt"
	"log/slog"
	"net"
	"sync/atomic"
)

type server struct {
	listener net.Listener
	logger   *slog.Logger

	started atomic.Bool
}

func NewServer(listener net.Listener, logger *slog.Logger) *server {
	return &server{
		listener: listener,
		logger:   logger,
		started:  atomic.Bool{},
	}
}

func (s *server) Start() error {
	if !s.started.CompareAndSwap(false, true) {
		return fmt.Errorf("server already started")
	}

	s.logger.Info("server started")

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			break
		}

		go s.handleConn(conn)
	}

	return nil
}

func (s *server) Stop() error {
	if err := s.listener.Close(); err != nil {
		s.logger.Error("cannot stop listener", slog.String("err", err.Error()))
	}

	return nil
}

func (s *server) handleConn(conn net.Conn) {
	for {
		buff := make([]byte, 4096)
		n, err := conn.Read(buff)
		if err != nil {
			break
		}

		if n == 0 {
			break
		}

		if _, err := conn.Write(buff[:n]); err != nil {
			// Handler todo error
			break
		}
	}
}
