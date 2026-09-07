package ws

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"chat-backend/internal/auth"
	"chat-backend/internal/booker"
	"chat-backend/internal/chat"
	"chat-backend/internal/llm"
	"chat-backend/internal/store"
)

// LLMTimeout caps how long we wait for the Booker gRPC service to respond.
const LLMTimeout = 30 * time.Second

type Server struct {
	store  *store.PostgresStore
	hub    *Hub
	auth   *auth.Service
	booker booker.Booker
}

type IncomingMessage struct {
	RecipientUsername string `json:"recipient_username"`
	Content           string `json:"content"`
}

func NewServer(s *store.PostgresStore, h *Hub, a *auth.Service, b booker.Booker) *Server {
	return &Server{store: s, hub: h, auth: a, booker: b}
}

func (s *Server) HandleWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	userID, err := s.auth.ValidateToken(token)
	if err != nil {
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
		return
	}

	opts := &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	}

	conn, err := websocket.Accept(w, r, opts)
	if err != nil {
		log.Println("Error accepting websocket connection:", err)
		return
	}
	defer conn.CloseNow()

	client := s.hub.Register(userID, conn)
	defer s.hub.Unregister(userID)

	log.Printf("user %d connected\n", userID)

	go func() {
		for {
			select {
			case message, ok := <-client.Send:
				if !ok {
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := conn.Write(ctx, websocket.MessageText, message)
				cancel()
				if err != nil {
					log.Printf("write error for user %d: %v\n", userID, err)
					return
				}
			case <-time.After(5 * time.Minute):
				pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := conn.Ping(pingCtx)
				cancel()
				if err != nil {
					log.Printf("ping error for user %d: %v\n", userID, err)
					return
				}
			}
		}
	}()

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		_, data, err := conn.Read(ctx)
		cancel()

		if err != nil {
			log.Printf("user %d disconnected: %v\n", userID, err)
			return
		}

		var incoming IncomingMessage
		if err := json.Unmarshal(data, &incoming); err != nil {
			log.Println("invalid message JSON:", err)
			continue
		}

		if incoming.Content == "" {
			log.Printf("user %d sent empty content, dropping\n", userID)
			continue
		}

		// Route messages addressed to the reserved "llm" recipient to the
		// Booker AI service. The user's message and the LLM reply are both
		// persisted in the messages table so history works transparently.
		if llm.IsLLM(incoming.RecipientUsername) {
			s.handleLLMMessage(client, userID, incoming.Content)
			continue
		}

		recipientUser, err := s.store.GetUserByUsername(context.Background(), incoming.RecipientUsername)
		if err != nil {
			log.Printf("recipient %s not found: %v\n", incoming.RecipientUsername, err)
			continue
		}
		recipientID := recipientUser.ID

		log.Printf("received from user %d to %s (ID: %d): %s\n", userID, incoming.RecipientUsername, recipientID, incoming.Content)

		convID := chat.ConversationID(userID, recipientID)
		saved, err := s.store.InsertMessage(context.Background(), convID, userID, incoming.Content)
		if err != nil {
			log.Println("insert error:", err)
			continue
		}

		senderUser, err := s.store.GetUserByID(context.Background(), userID)
		if err != nil {
			log.Println("sender lookup error:", err)
		}

		pushPayload := map[string]interface{}{
			"id":              saved.ID,
			"conversation_id": saved.ConversationID,
			"sender_id":       saved.SenderID,
			"sender_username": senderUser.Username,
			"content":         saved.Content,
			"created_at":      saved.CreatedAt.Format(time.RFC3339Nano),
		}
		pushBytes, _ := json.Marshal(pushPayload)

		if recipientClient, online := s.hub.GetClient(recipientID); online {
			select {
			case recipientClient.Send <- pushBytes:
				log.Printf("pushed to user ID %d\n", recipientID)
			default:
				log.Printf("queue full for user %d, dropping message\n", recipientID)
			}
		} else {
			log.Printf("user %d offline, message saved only\n", recipientID)
		}
	}
}

// handleLLMMessage persists the user's message to the LLM, calls the Booker
// AI service, persists the reply, and pushes the reply back over the user's
// websocket. Any error from the AI service is reported back to the user as a
// chat reply rather than dropping the message silently.
func (s *Server) handleLLMMessage(client *Client, userID int64, content string) {
	llmUser, err := s.store.GetUserByUsername(context.Background(), llm.Username)
	if err != nil {
		log.Printf("llm user not found (run migrations/0002_llm_user.sql): %v", err)
		s.pushLLMError(client, userID, "assistant is currently unavailable")
		return
	}
	llmID := llmUser.ID

	convID := chat.ConversationID(userID, llmID)

	saved, err := s.store.InsertMessage(context.Background(), convID, userID, content)
	if err != nil {
		log.Printf("insert user->llm message error: %v", err)
		s.pushLLMError(client, userID, "failed to save your message")
		return
	}

	if user, err := s.store.GetUserByID(context.Background(), userID); err == nil {
		s.pushTo(client, map[string]interface{}{
			"id":              saved.ID,
			"conversation_id": saved.ConversationID,
			"sender_id":       saved.SenderID,
			"sender_username": user.Username,
			"content":         saved.Content,
			"created_at":      saved.CreatedAt.Format(time.RFC3339Nano),
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), LLMTimeout)
	defer cancel()

	resp, err := s.booker.ProcessUserMessage(ctx, strconv.FormatInt(userID, 10), content)
	if err != nil {
		log.Printf("booker rpc error user=%d err=%v", userID, err)
		s.pushLLMError(client, userID, "assistant is currently unavailable")
		return
	}

	replySaved, err := s.store.InsertMessage(context.Background(), convID, llmID, resp.Reply)
	if err != nil {
		log.Printf("insert llm reply error: %v", err)
		s.pushLLMError(client, userID, "failed to save assistant reply")
		return
	}

	s.pushTo(client, map[string]interface{}{
		"id":              replySaved.ID,
		"conversation_id": replySaved.ConversationID,
		"sender_id":       replySaved.SenderID,
		"sender_username": llmUser.Username,
		"content":         replySaved.Content,
		"tool_executed":   resp.ToolExecuted,
		"created_at":      replySaved.CreatedAt.Format(time.RFC3339Nano),
	})
	log.Printf("llm reply delivered to user %d (tool_executed=%v)\n", userID, resp.ToolExecuted)
}

// pushTo marshals payload and enqueues it on the given client's send channel,
// dropping it if the channel is full (the same backpressure policy used for
// regular DMs in this server).
func (s *Server) pushTo(client *Client, payload map[string]interface{}) {
	b, err := json.Marshal(payload)
	if err != nil {
		log.Printf("push marshal error: %v", err)
		return
	}
	select {
	case client.Send <- b:
	default:
		log.Printf("queue full for user %d, dropping push\n", client.UserID)
	}
}

// pushLLMError sends a synthetic assistant message back to the user when the
// AI service or persistence layer fails. It uses the real llm user id so it
// renders like any other assistant reply.
func (s *Server) pushLLMError(client *Client, userID int64, msg string) {
	llmUser, err := s.store.GetUserByUsername(context.Background(), llm.Username)
	if err != nil {
		return
	}
	s.pushTo(client, map[string]interface{}{
		"conversation_id": chat.ConversationID(userID, llmUser.ID),
		"sender_id":       llmUser.ID,
		"sender_username": llmUser.Username,
		"content":         msg,
		"created_at":      time.Now().Format(time.RFC3339Nano),
	})
}