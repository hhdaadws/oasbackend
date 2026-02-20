package server

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"oas-cloud-go/internal/auth"
	"oas-cloud-go/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ScanWSMessage is the JSON message pushed to user WebSocket clients.
type ScanWSMessage struct {
	Type       string `json:"type"`
	Phase      string `json:"phase,omitempty"`
	Screenshot string `json:"screenshot,omitempty"`
	ChoiceType string `json:"choice_type,omitempty"`
	LoginID    string `json:"login_id,omitempty"`
	Message    string `json:"message,omitempty"`
	Position   int    `json:"position,omitempty"`
}

// ScanWSClient represents a single WebSocket connection for a user.
type ScanWSClient struct {
	userID uint
	conn   *websocket.Conn
	send   chan []byte
	done   chan struct{}
}

// ScanWSHub manages active WebSocket connections for scan status updates.
type ScanWSHub struct {
	mu      sync.RWMutex
	clients map[uint]*ScanWSClient
}

func newScanWSHub() *ScanWSHub {
	return &ScanWSHub{clients: make(map[uint]*ScanWSClient)}
}

// Register adds a new WebSocket client, replacing any existing connection for the same user.
func (h *ScanWSHub) Register(userID uint, conn *websocket.Conn) *ScanWSClient {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old, ok := h.clients[userID]; ok {
		close(old.done)
		old.conn.Close()
	}
	client := &ScanWSClient{
		userID: userID,
		conn:   conn,
		send:   make(chan []byte, 16),
		done:   make(chan struct{}),
	}
	h.clients[userID] = client
	return client
}

// Unregister removes a WebSocket client for the given user.
func (h *ScanWSHub) Unregister(userID uint) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.clients[userID]; ok {
		close(c.done)
		c.conn.Close()
		delete(h.clients, userID)
	}
}

// NotifyUser sends a message to the given user's WebSocket connection.
func (h *ScanWSHub) NotifyUser(userID uint, msg ScanWSMessage) {
	h.mu.RLock()
	client, ok := h.clients[userID]
	h.mu.RUnlock()
	if !ok {
		return
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	select {
	case client.send <- data:
	default:
		// buffer full, skip
	}
}

// WritePump runs the write loop for the client, sending messages and pings.
func (c *ScanWSClient) WritePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	defer c.conn.Close()
	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}

// ReadPump runs the read loop, keeping the connection alive and detecting disconnects.
func (c *ScanWSClient) ReadPump(hub *ScanWSHub) {
	defer hub.Unregister(c.userID)
	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// userScanWS handles WebSocket upgrade for scan real-time updates.
func (s *Server) userScanWS(c *gin.Context) {
	tokenStr := c.Query("token")
	if tokenStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "missing token"})
		return
	}

	hash := auth.HashToken(tokenStr)
	now := time.Now().UTC()
	var token models.UserToken
	if err := s.db.Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", hash, now).First(&token).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "invalid token"})
		return
	}
	var user models.User
	if err := s.db.Where("id = ? AND status = ?", token.UserID, models.UserStatusActive).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "invalid user"})
		return
	}

	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}

	client := s.scanWSHub.Register(user.ID, conn)
	go client.WritePump()
	go client.ReadPump(s.scanWSHub)
}
