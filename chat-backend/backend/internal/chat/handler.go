package chat

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"chat-backend/internal/auth"
)

func (c *Service) HandleGetHistory(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	friendID, err := strconv.ParseInt(r.URL.Query().Get("friend_id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid friend_id", http.StatusBadRequest)
		return
	}

	limit := 20
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil {
			limit = parsed
		}
	}

	convID := ConversationID(userID, friendID)

	messages, err := c.GetHistory(r.Context(), convID, limit)
	if err != nil {
		log.Printf("get history error user=%d conv=%d err=%v", userID, convID, err)
		http.Error(w, "failed to fetch history", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(messages)
}