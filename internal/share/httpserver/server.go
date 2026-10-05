// Package httpserver assembles the router and manages the HTTP server lifecycle.
package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
)

// Server owns the HTTP listener and its graceful shutdown.
type Server struct {
	srv             *http.Server
	logger          *slog.Logger
	shutdownTimeout time.Duration
}

// New creates a server bound to the configured address.
func New(cfg *config.Config, logger *slog.Logger, handler http.Handler) *Server {
	return &Server{
		srv: &http.Server{
			Addr:         cfg.HTTP.Addr,
			Handler:      handler,
			ReadTimeout:  cfg.HTTP.ReadTimeout,
			WriteTimeout: cfg.HTTP.WriteTimeout,
			IdleTimeout:  cfg.HTTP.IdleTimeout,
		},
		logger:          logger,
		shutdownTimeout: cfg.HTTP.ShutdownTimeout,
	}
}

// Start blocks until the server stops. A clean shutdown returns nil.
func (s *Server) Start() error {
	s.logger.Info("http server listening", slog.String("addr", s.srv.Addr))
	if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.shutdownTimeout)
	defer cancel()
	return s.srv.Shutdown(ctx)
}
