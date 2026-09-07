package store

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func Connect() (*pgxpool.Pool, error) {
	godotenv.Load()

	dbUrl := os.Getenv("DATABASE_URL")
	if dbUrl == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set")
	}

	pool, err := pgxpool.New(context.Background(), dbUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %v", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ping database: %v", err)
	}

	return pool, nil
}
func (s *PostgresStore) AreFriends(ctx context.Context, userID, friendID int64) (bool, error) {
    var exists bool
    query := `
        SELECT EXISTS (
            SELECT 1 FROM friendships 
            WHERE ((user_id = $1 AND friend_id = $2) OR (user_id = $2 AND friend_id = $1))
              AND status = 'accepted'
        )
    `
    err := s.pool.QueryRow(ctx, query, userID, friendID).Scan(&exists)
    return exists, err
}