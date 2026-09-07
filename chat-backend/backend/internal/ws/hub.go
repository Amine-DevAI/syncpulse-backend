package ws

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type Client struct {
	Conn   *websocket.Conn
	UserID int64
	Send   chan []byte
}

type Hub struct {
	mu      sync.RWMutex
	clients map[int64]*Client
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[int64]*Client),
	}
}

func (h *Hub) Register(userID int64, conn *websocket.Conn) *Client {
	h.mu.Lock()
	defer h.mu.Unlock()

	client := &Client{
		Conn:   conn,
		UserID: userID,
		Send:   make(chan []byte, 256),
	}

	h.clients[userID] = client
	return client
}

func (h *Hub) Unregister(userID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if client, exists := h.clients[userID]; exists {
		close(client.Send)
		delete(h.clients, userID)
	}
}

func (h *Hub) GetClient(userID int64) (*Client, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	client, exists := h.clients[userID]
	return client, exists
}

func (h *Hub) IsOnline(userID int64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	_, exists := h.clients[userID]
	return exists
}

func (h *Hub) Broadcast(message []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		select {
		case client.Send <- message:
		default:
		}
	}
}

func (h *Hub) Cleanup() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for userID, client := range h.clients {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := client.Conn.Ping(ctx)
		cancel()

		if err != nil {
			close(client.Send)
			delete(h.clients, userID)
		}
	}
}
