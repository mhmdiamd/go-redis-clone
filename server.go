package goredis

import (
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

		started: atomic.Bool{},
	}
}

func (s *server) Start() error {
	panic("todo")
}

func (s *server) Stop() error {
	panic("todo")
}
