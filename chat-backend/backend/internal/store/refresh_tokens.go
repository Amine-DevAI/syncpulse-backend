package store

import (
	"context"
	"time"
)

func (s *PostgresStore) InsertRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, tokenHash, expiresAt,
	)
	return err
}

type RefreshToken struct {
	ID        int64
	UserID    int64
	TokenHash string
	ExpiresAt time.Time
	Revoked   bool
}

func (s *PostgresStore) GetRefreshToken(ctx context.Context, tokenHash string) (RefreshToken, error) {
	var rt RefreshToken
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, token_hash, expires_at, revoked FROM refresh_tokens WHERE token_hash = $1`,
		tokenHash,
	).Scan(&rt.ID, &rt.UserID, &rt.TokenHash, &rt.ExpiresAt, &rt.Revoked)
	return rt, err
}

func (s *PostgresStore) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked = true WHERE token_hash = $1`,
		tokenHash,
	)
	return err
}

func (s *PostgresStore) RevokeAllUserRefreshTokens(ctx context.Context, userID int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked = true WHERE user_id = $1`,
		userID,
	)
	return err
}