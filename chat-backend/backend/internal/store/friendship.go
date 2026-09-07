package store

import (
    "context"
)

func (s *PostgresStore) AddFriendship(ctx context.Context, userID, friendID int64, status string) error {
    query := `
        INSERT INTO friendships (user_id, friend_id, status) 
        VALUES ($1, $2, $3)
        ON CONFLICT (user_id, friend_id) 
        DO UPDATE SET status = EXCLUDED.status
    `
    _, err := s.pool.Exec(ctx, query, userID, friendID, status)
    return err
}

func (s *PostgresStore) GetFriends(ctx context.Context, userID int64) ([]User, error) {
    query := `
        SELECT u.id, u.username
        FROM friendships f
        JOIN users u ON u.id = f.friend_id
        WHERE f.user_id = $1 AND f.status = 'accepted'
        UNION
        SELECT u.id, u.username
        FROM friendships f
        JOIN users u ON u.id = f.user_id
        WHERE f.friend_id = $1 AND f.status = 'accepted'
    `
    rows, err := s.pool.Query(ctx, query, userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var friends []User
    for rows.Next() {
        var u User
        if err := rows.Scan(&u.ID, &u.Username); err != nil {
            return nil, err
        }
        friends = append(friends, u)
    }

    return friends, rows.Err()
}