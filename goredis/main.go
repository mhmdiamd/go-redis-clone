package main

import (
	"log/slog"
	goredisclone "mhmdiamd/go-redis-clone"
	"net"
	"os"
	"os/signal"
)

func main() {
	// Create logger first
	logger := slog.New(slog.NewTextHandler(
		os.Stderr,
		&slog.HandlerOptions{
			Level: slog.LevelDebug,
		},
	))

	// logger for starting server
	address := "0.0.0.0:3100"
	logger.Info("starting server", slog.String("Address", address))

	// create server listener, with error handler
	listener, err := net.Listen("tcp", address)
	if err != nil {
		logger.Error(
			"cannot start tcp server",
			slog.String("address", address),
			slog.String("err", err.Error()),
		)
	}

	server := goredisclone.NewServer(listener, logger)

	// Running server in the background with goroutine
	go func() {
		if err := server.Start(); err != nil {
			logger.Error("server error", slog.String("err", err.Error()))
			os.Exit(1)
		}
	}()

	// handler for stoping server when user click ctrl + c killing terminal process
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c

	if err := server.Stop(); err != nil {
		logger.Error("cannot stop server", slog.String("err", err.Error()))
		os.Exit(1)
	}
}
