package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

type Server struct {
	port     int
	client   *Client
	server   *http.Server
	listener net.Listener
}

func NewServer(port int, client *Client) *Server {
	return &Server{port: port, client: client}
}

func (s *Server) Port() int {
	if s.listener != nil {
		return s.listener.Addr().(*net.TCPAddr).Port
	}
	return s.port
}

func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /status", s.handleStatus)

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", s.port))
	if err != nil {
		return fmt.Errorf("daemon server listen: %w", err)
	}
	s.listener = ln
	s.server = &http.Server{Handler: mux}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.server.Shutdown(shutCtx)
	}()

	if err := s.server.Serve(ln); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"is_connected": s.client.IsConnected(),
		"active_agent": s.client.ActiveAgent(),
		"uptime":       int(s.client.Uptime().Seconds()),
	})
}
