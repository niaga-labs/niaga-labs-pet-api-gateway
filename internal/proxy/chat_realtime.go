package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Kilat-Pet-Delivery/lib-common/auth"
	"github.com/Kilat-Pet-Delivery/lib-common/kafka"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	kafkago "github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

const (
	chatWriteWait      = 10 * time.Second
	chatPongWait       = 60 * time.Second
	chatPingPeriod     = (chatPongWait * 9) / 10
	chatMaxMessageSize = 16 * 1024

	chatTopicEvents = "chat.events"
	chatMessageSent = "chat.message_sent"
	chatMessageRead = "chat.message_read"
	chatTyping      = "chat.typing"
)

type chatThreadEvent struct {
	ThreadID uuid.UUID `json:"thread_id"`
}

// ChatRealtime owns gateway-side chat and presence sockets.
type ChatRealtime struct {
	chatUpstream string
	kafkaBrokers []string
	gatewayID    string
	jwtManager   *auth.JWTManager
	httpClient   *http.Client
	logger       *zap.Logger
	upgrader     websocket.Upgrader

	mu      sync.RWMutex
	threads map[uuid.UUID]map[*realtimeClient]bool
}

// NewChatRealtime creates a gateway realtime chat handler.
func NewChatRealtime(chatUpstream string, kafkaBrokers []string, gatewayID string, jwtManager *auth.JWTManager, logger *zap.Logger) *ChatRealtime {
	return &ChatRealtime{
		chatUpstream: strings.TrimRight(chatUpstream, "/"),
		kafkaBrokers: kafkaBrokers,
		gatewayID:    gatewayID,
		jwtManager:   jwtManager,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		logger:       logger,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		threads: make(map[uuid.UUID]map[*realtimeClient]bool),
	}
}

// Start begins consuming chat.events for gateway fan-out.
func (r *ChatRealtime) Start(ctx context.Context) {
	if len(r.kafkaBrokers) == 0 || r.kafkaBrokers[0] == "" {
		r.logger.Warn("chat realtime disabled; KAFKA_BROKERS is empty")
		return
	}

	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:  r.kafkaBrokers,
		GroupID:  "api-gateway-chat-" + r.gatewayID,
		Topic:    chatTopicEvents,
		MinBytes: 1,
		MaxBytes: 10e6,
	})

	go func() {
		defer func() { _ = reader.Close() }()
		for {
			msg, err := reader.FetchMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				r.logger.Error("failed to fetch chat event", zap.Error(err))
				continue
			}
			if err := r.handleChatEvent(ctx, msg); err != nil {
				r.logger.Error("failed to handle chat event", zap.Error(err))
				continue
			}
			if err := reader.CommitMessages(ctx, msg); err != nil {
				r.logger.Error("failed to commit chat event", zap.Error(err))
			}
		}
	}()
}

// HandleChatWS upgrades a client into the chat socket.
func (r *ChatRealtime) HandleChatWS(c *gin.Context) {
	token := c.Query("token")
	claims, ok := r.validateToken(c, token)
	if !ok {
		return
	}

	conn, err := r.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		r.logger.Error("failed to upgrade chat websocket", zap.Error(err))
		return
	}

	client := &realtimeClient{
		conn:   conn,
		send:   make(chan []byte, 256),
		userID: claims.UserID,
		token:  token,
	}
	_ = r.markPresence(c.Request.Context(), token, "online")
	defer func() {
		_ = r.markPresence(context.Background(), token, "offline")
		r.unregisterAll(client)
		_ = conn.Close()
	}()

	go client.writePump()
	r.readChatPump(c.Request.Context(), client)
}

// HandlePresenceWS upgrades a client into the presence socket.
func (r *ChatRealtime) HandlePresenceWS(c *gin.Context) {
	token := c.Query("token")
	if _, ok := r.validateToken(c, token); !ok {
		return
	}

	conn, err := r.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		r.logger.Error("failed to upgrade presence websocket", zap.Error(err))
		return
	}
	defer func() { _ = conn.Close() }()

	_ = r.markPresence(c.Request.Context(), token, "online")
	defer func() { _ = r.markPresence(context.Background(), token, "offline") }()

	_ = conn.WriteJSON(gin.H{"type": "presence_ready"})
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (r *ChatRealtime) readChatPump(ctx context.Context, client *realtimeClient) {
	defer close(client.send)
	client.conn.SetReadLimit(chatMaxMessageSize)
	_ = client.conn.SetReadDeadline(time.Now().Add(chatPongWait))
	client.conn.SetPongHandler(func(string) error {
		_ = client.conn.SetReadDeadline(time.Now().Add(chatPongWait))
		return nil
	})

	for {
		var msg chatClientMessage
		if err := client.conn.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				r.logger.Warn("chat websocket read error", zap.Error(err))
			}
			return
		}

		switch msg.Type {
		case "subscribe":
			r.register(client, msg.ThreadID)
		case "send_message":
			r.register(client, msg.ThreadID)
			if err := r.forwardJSON(ctx, client.token, http.MethodPost, fmt.Sprintf("/api/v1/threads/%s/messages", msg.ThreadID), msg); err != nil {
				r.logger.Error("failed to forward chat message", zap.Error(err))
			}
		case "typing":
			r.register(client, msg.ThreadID)
			if err := r.forwardJSON(ctx, client.token, http.MethodPost, fmt.Sprintf("/api/v1/threads/%s/typing", msg.ThreadID), gin.H{}); err != nil {
				r.logger.Error("failed to forward typing event", zap.Error(err))
			}
		}
	}
}

func (r *ChatRealtime) handleChatEvent(ctx context.Context, msg kafkago.Message) error {
	cloudEvent, err := kafka.ParseCloudEvent(msg.Value)
	if err != nil {
		return err
	}

	var threadID uuid.UUID
	switch cloudEvent.Type {
	case chatMessageSent:
		var event chatThreadEvent
		if err := cloudEvent.ParseData(&event); err != nil {
			return err
		}
		threadID = event.ThreadID
	case chatMessageRead:
		var event chatThreadEvent
		if err := cloudEvent.ParseData(&event); err != nil {
			return err
		}
		threadID = event.ThreadID
	case chatTyping:
		var event chatThreadEvent
		if err := cloudEvent.ParseData(&event); err != nil {
			return err
		}
		threadID = event.ThreadID
	default:
		return nil
	}

	payload, err := json.Marshal(gin.H{
		"type": cloudEvent.Type,
		"data": json.RawMessage(cloudEvent.Data),
	})
	if err != nil {
		return err
	}
	r.broadcast(threadID, payload)
	return nil
}

func (r *ChatRealtime) register(client *realtimeClient, threadID uuid.UUID) {
	if threadID == uuid.Nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.threads[threadID]; !ok {
		r.threads[threadID] = make(map[*realtimeClient]bool)
	}
	r.threads[threadID][client] = true
}

func (r *ChatRealtime) unregisterAll(client *realtimeClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for threadID, clients := range r.threads {
		delete(clients, client)
		if len(clients) == 0 {
			delete(r.threads, threadID)
		}
	}
}

func (r *ChatRealtime) broadcast(threadID uuid.UUID, payload []byte) {
	r.mu.RLock()
	clients := r.threads[threadID]
	r.mu.RUnlock()
	for client := range clients {
		select {
		case client.send <- payload:
		default:
			r.unregisterAll(client)
			_ = client.conn.Close()
		}
	}
}

func (r *ChatRealtime) validateToken(c *gin.Context, token string) (*auth.Claims, bool) {
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "token query parameter is required"})
		return nil, false
	}
	claims, err := r.jwtManager.ValidateAccessToken(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
		return nil, false
	}
	return claims, true
}

func (r *ChatRealtime) markPresence(ctx context.Context, token string, state string) error {
	return r.forwardJSON(ctx, token, http.MethodPost, "/api/v1/presence/"+state, gin.H{"gateway_id": r.gatewayID})
}

func (r *ChatRealtime) forwardJSON(ctx context.Context, token string, method string, path string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, r.chatUpstream+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("service-chat returned %s for %s", resp.Status, path)
	}
	return nil
}

type realtimeClient struct {
	conn   *websocket.Conn
	send   chan []byte
	userID uuid.UUID
	token  string
}

func (c *realtimeClient) writePump() {
	ticker := time.NewTicker(chatPingPeriod)
	defer ticker.Stop()
	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(chatWriteWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(chatWriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

type chatClientMessage struct {
	Type           string    `json:"type"`
	ThreadID       uuid.UUID `json:"thread_id"`
	Body           string    `json:"body,omitempty"`
	AttachmentURL  string    `json:"attachment_url,omitempty"`
	AttachmentMIME string    `json:"attachment_mime,omitempty"`
}
