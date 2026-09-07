package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"chat-backend/internal/auth"
	"chat-backend/internal/booker"
	"chat-backend/internal/chat"
	"chat-backend/internal/store"
	"chat-backend/internal/ws"
)

func main() {
	pool, err := store.Connect()
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close()

	s := store.NewPostgresStore(pool)

	authService := auth.NewService(s)
	chatService := chat.NewService(s)

	bookerAddr := os.Getenv("BOOKER_GRPC_ADDR")
	if bookerAddr == "" {
		log.Fatal("BOOKER_GRPC_ADDR is not set")
	}
	bookerClient, err := booker.NewClient(booker.Config{Address: bookerAddr})
	if err != nil {
		log.Fatalf("failed to create booker client: %v", err)
	}
	defer bookerClient.Close()

	mux := http.NewServeMux()

	// public — no token required
	mux.HandleFunc("POST /users", authService.HandleCreateUser)
	mux.HandleFunc("POST /login", authService.HandleLogin)
	mux.HandleFunc("POST /refresh", authService.HandleRefresh)

	// protected — access token required
	mux.HandleFunc("POST /friends", authService.RequireAuth(authService.HandleAddFriend))
	mux.HandleFunc("GET /friends", authService.RequireAuth(authService.HandleGetFriends))
	mux.HandleFunc("GET /assistant", authService.RequireAuth(authService.HandleAssistant))
	mux.HandleFunc("GET /history", authService.RequireAuth(chatService.HandleGetHistory))
	mux.HandleFunc("POST /logout", authService.RequireAuth(authService.HandleLogout))
	mux.HandleFunc("POST /booker", authService.RequireAuth(booker.NewHandler(bookerClient).HandleBooker))

	hub := ws.NewHub()
	wsServer := ws.NewServer(s, hub, authService, bookerClient)

	mux.HandleFunc("/ws", wsServer.HandleWS)

	// Periodic cleanup of dead connections
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			hub.Cleanup()
		}
	}()

	// Wrap the entire mux with CORS so OPTIONS preflight requests are
	// handled for every route. Registering `POST /login` in ServeMux
	// does NOT match an incoming `OPTIONS /login` — it returns 405
	// before any per-handler middleware runs. A single outer CORS
	// layer short-circuits OPTIONS first.
	handler := ws.CORSMiddleware(mux)

	fmt.Println("Server listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", handler))
}
