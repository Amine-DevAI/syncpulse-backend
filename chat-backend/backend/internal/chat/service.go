package chat

import (
	"context"

	"chat-backend/internal/store"
)

type Service struct {
	store *store.PostgresStore
}

func NewService(s *store.PostgresStore) *Service {
	return &Service{store: s}
}

func ConversationID(userA, userB int64) int64 {
	if userA > userB {
		userA, userB = userB, userA
	}
	return userA*1_000_000 + userB
}

func (c *Service) GetHistory(ctx context.Context, conversationID int64, limit int) ([]store.Message, error) {
	return c.store.GetHistory(ctx, conversationID, limit)
}