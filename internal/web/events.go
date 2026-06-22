package web

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// broadcaster is a tiny SSE fan-out: handlers publish a message and every
// connected /events client receives it. It replaces the old per-second status
// polling (which spammed the network tab) with a single long-lived stream.
type broadcaster struct {
	mu     sync.Mutex
	subs   map[chan string]struct{}
	closed bool
}

func newBroadcaster() *broadcaster {
	return &broadcaster{subs: make(map[chan string]struct{})}
}

func (b *broadcaster) subscribe() chan string {
	ch := make(chan string, 8)
	b.mu.Lock()
	defer b.mu.Unlock()
	// Already shutting down: hand back a closed channel so handleEvents returns
	// at once instead of opening a stream that would block Shutdown.
	if b.closed {
		close(ch)
		return ch
	}
	b.subs[ch] = struct{}{}
	return ch
}

// close ends every active stream. It's wired to the http.Server shutdown so the
// long-lived /events connections don't keep Shutdown blocked until its deadline.
func (b *broadcaster) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for ch := range b.subs {
		delete(b.subs, ch)
		close(ch)
	}
}

func (b *broadcaster) unsubscribe(ch chan string) {
	b.mu.Lock()
	if _, ok := b.subs[ch]; ok {
		delete(b.subs, ch)
		close(ch)
	}
	b.mu.Unlock()
}

// publish delivers msg to every subscriber, dropping it for any consumer whose
// buffer is full rather than blocking the caller (status updates are advisory).
func (b *broadcaster) publish(msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- msg:
		default:
		}
	}
}

// handleEvents is the single SSE stream the playground page subscribes to. It
// carries generate-status events; the native EventSource on the client routes
// them into the existing Alpine generation-status handler.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := s.events.subscribe()
	defer s.events.unsubscribe(ch)

	// Comment ping keeps idle connections alive through proxies/buffers.
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case msg, ok := <-ch:
			if !ok {
				return
			}
			_, _ = fmt.Fprintf(w, "event: generate-status\ndata: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

// StopEvents ends all SSE streams. Register it with http.Server.RegisterOnShutdown
// so graceful shutdown isn't blocked waiting on the long-lived /events connections.
func (s *Server) StopEvents() {
	if s.events != nil {
		s.events.close()
	}
}

// publishGenerateStatus pushes one AI-generation progress update to all clients.
func (s *Server) publishGenerateStatus(running bool, stage, detail string) {
	if s.events == nil {
		return
	}
	s.events.publish(`{"running":` + strconv.FormatBool(running) +
		`,"stage":` + strconv.Quote(stage) +
		`,"detail":` + strconv.Quote(detail) + `}`)
}
