package store

import (
	"context"
    "time"
	"github.com/jackc/pgx/v5/pgxpool"
)


type Message struct {
	ID             int64
	ConversationID int64
	SenderID       int64
	Content        string
	CreatedAt      time.Time
}
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}


func (s *PostgresStore) InsertMessage(ctx context.Context, conversationID, senderID int64, content string) (Message, error) {
	var m Message
	err := s.pool.QueryRow(ctx,
		`INSERT INTO messages (conversation_id, sender_id, content) VALUES ($1, $2, $3)
		 RETURNING id, conversation_id, sender_id, content, created_at`,
		conversationID, senderID, content,
	).Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Content, &m.CreatedAt)
	if err != nil {
		return Message{}, err
	}
	return m, nil
}

func (s *PostgresStore) InsertUser(ctx context.Context, username, passwordHash string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id`,
		username, passwordHash,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (s *PostgresStore) GetHistory(ctx context.Context, conversationID int64, limit int) ([]Message, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, conversation_id, sender_id, content, created_at
		 FROM messages
		 WHERE conversation_id = $1
		 ORDER BY created_at DESC, id DESC
		 LIMIT $2`,
		conversationID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages = make([]Message, 0)
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}

	return messages, rows.Err()
}
type User struct {
    ID           int64
    Username     string
    PasswordHash string
}

func (s *PostgresStore) GetUserByUsername(ctx context.Context, username string) (User, error) {
    var u User
    query := `SELECT id, username, password_hash FROM users WHERE username = $1`
    err := s.pool.QueryRow(ctx, query, username).Scan(&u.ID, &u.Username, &u.PasswordHash)
    if err != nil {
        return User{}, err
    }
    return u, nil
}

func (s *PostgresStore) GetUserByID(ctx context.Context, id int64) (User, error) {
    var u User
    query := `SELECT id, username, password_hash FROM users WHERE id = $1`
    err := s.pool.QueryRow(ctx, query, id).Scan(&u.ID, &u.Username, &u.PasswordHash)
    if err != nil {
        return User{}, err
    }
    return u, nil
}