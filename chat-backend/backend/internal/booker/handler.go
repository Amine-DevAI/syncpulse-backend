package booker

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"chat-backend/internal/auth"
)

type bookerRequest struct {
	Message string `json:"message"`
}

type bookerResponse struct {
	Reply        string `json:"reply"`
	ToolExecuted bool   `json:"tool_executed"`
}

type Booker interface {
	ProcessUserMessage(ctx context.Context, userID, message string) (*Response, error)
}

type Handler struct {
	client Booker
}

func NewHandler(c Booker) *Handler {
	return &Handler{client: c}
}

func (h *Handler) HandleBooker(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req bookerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	uid := strconv.FormatInt(userID, 10)
	resp, err := h.client.ProcessUserMessage(ctx, uid, req.Message)
	if err != nil {
		log.Printf("booker rpc error user=%s err=%v", uid, err)
		http.Error(w, "booker service unavailable", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(bookerResponse{
		Reply:        resp.Reply,
		ToolExecuted: resp.ToolExecuted,
	})
}
