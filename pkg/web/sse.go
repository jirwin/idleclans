package web

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

// SSEBroker manages Server-Sent Events connections
type SSEBroker struct {
	clients    map[chan string]bool
	register   chan chan string
	unregister chan chan string
	broadcast  chan string
	mu         sync.RWMutex
	logger     *zap.Logger
}

// NewSSEBroker creates a new SSE broker
func NewSSEBroker(logger *zap.Logger) *SSEBroker {
	broker := &SSEBroker{
		clients:    make(map[chan string]bool),
		register:   make(chan chan string),
		unregister: make(chan chan string),
		broadcast:  make(chan string, 100),
		logger:     logger,
	}
	go broker.run()
	return broker
}

func (b *SSEBroker) run() {
	for {
		select {
		case client := <-b.register:
			b.mu.Lock()
			b.clients[client] = true
			b.mu.Unlock()
			b.logger.Debug("SSE client connected", zap.Int("total_clients", len(b.clients)))

		case client := <-b.unregister:
			b.mu.Lock()
			if _, ok := b.clients[client]; ok {
				delete(b.clients, client)
				close(client)
			}
			b.mu.Unlock()
			b.logger.Debug("SSE client disconnected", zap.Int("total_clients", len(b.clients)))

		case message := <-b.broadcast:
			b.mu.RLock()
			for client := range b.clients {
				select {
				case client <- message:
				default:
					// Client buffer full, skip
				}
			}
			b.mu.RUnlock()
		}
	}
}

// Broadcast sends a message to all connected clients
func (b *SSEBroker) Broadcast(eventType string) {
	b.logger.Info("Broadcasting SSE event",
		zap.String("type", eventType),
		zap.Int("clients", b.ClientCount()))
	select {
	case b.broadcast <- eventType:
	default:
		// Broadcast channel full, skip
		b.logger.Warn("SSE broadcast channel full, skipping")
	}
}

// ClientCount returns the number of connected clients
func (b *SSEBroker) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

// handleSSE handles SSE connections for the public frontend
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering for SSE

	// Check if response writer supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Create client channel
	client := make(chan string, 10)

	// Register client
	s.sseBroker.register <- client

	// Ensure client is unregistered on disconnect
	defer func() {
		s.sseBroker.unregister <- client
	}()

	// Get the request context for cancellation
	ctx := r.Context()

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {\"status\":\"connected\"}\n\n")
	flusher.Flush()

	// Keep-alive ticker (send ping every 15 seconds to prevent timeouts)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Send keep-alive ping
			_, err := fmt.Fprintf(w, ": ping\n\n")
			if err != nil {
				return // Client disconnected
			}
			flusher.Flush()
		case msg, ok := <-client:
			if !ok {
				return
			}
			_, err := fmt.Fprintf(w, "event: update\ndata: {\"type\":\"%s\"}\n\n", msg)
			if err != nil {
				return // Client disconnected
			}
			flusher.Flush()
		}
	}
}

// handleAdminSSE handles SSE connections for the admin frontend
func (s *Server) handleAdminSSE(w http.ResponseWriter, r *http.Request) {
	// Use the same handler - admin gets the same events
	s.handleSSE(w, r)
}

// NotifyDataChange broadcasts a data change event to all connected clients
func (s *Server) NotifyDataChange(changeType string) {
	if s.sseBroker != nil {
		s.sseBroker.Broadcast(changeType)
	}
}

