package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/panel/internal/api"
	"github.com/yourorg/panel/internal/auth"
	"github.com/yourorg/panel/internal/config"
	"github.com/yourorg/panel/internal/crypto"
	"github.com/yourorg/panel/internal/daemonclient"
	"github.com/yourorg/panel/internal/db"
	"github.com/yourorg/panel/internal/models"
	"github.com/yourorg/panel/internal/ratelimit"
	"github.com/yourorg/panel/internal/scheduler"
	"github.com/yourorg/panel/internal/ws"
)

var (
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()

	pool, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	tokenManager := auth.NewTokenManager(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	resolveNodeClient := nodeClientResolver(pool, cfg.EncryptionKey)
	limiter := ratelimit.New(cfg.RedisAddr, cfg.RedisPassword)

	resolveServerClient := func(ctx context.Context, serverUUID uuid.UUID) (*daemonclient.Client, error) {
		var nodeID int64
		if err := pool.QueryRow(ctx, `SELECT node_id FROM servers WHERE uuid = $1`, serverUUID).Scan(&nodeID); err != nil {
			return nil, err
		}
		return resolveNodeClient(nodeID)
	}

	hub := ws.NewHub()
	hub.FetchStats = func(ctx context.Context, serverUUID uuid.UUID) (*models.ResourceStats, error) {
		client, err := resolveServerClient(ctx, serverUUID)
		if err != nil {
			return nil, err
		}
		stats, err := client.Stats(ctx, serverUUID)
		if err != nil {
			return nil, err
		}
		return &models.ResourceStats{
			ServerUUID:    stats.ServerUUID,
			CPUPercent:    stats.CPUPercent,
			MemoryBytes:   stats.MemoryBytes,
			DiskBytes:     stats.DiskBytes,
			NetworkRx:     stats.NetworkRx,
			NetworkTx:     stats.NetworkTx,
			UptimeSeconds: stats.UptimeSeconds,
			State:         models.ServerStatus(stats.State),
		}, nil
	}
	hub.DialConsole = func(ctx context.Context, serverUUID uuid.UUID) (*websocket.Conn, error) {
		client, err := resolveServerClient(ctx, serverUUID)
		if err != nil {
			return nil, err
		}
		return client.DialConsole(ctx, serverUUID)
	}

	go scheduler.Run(pool, resolveNodeClient)

	router := api.NewRouter(api.Dependencies{
		DB:            pool,
		Token:         tokenManager,
		Hub:           hub,
		NodeClient:    resolveNodeClient,
		EncryptionKey: cfg.EncryptionKey,
		Limiter:       limiter,
		Commit:        commit,
		BuildDate:     buildDate,
		SourceDir:     cfg.SourceDir,
		RepoSlug:      cfg.RepoSlug,
	})

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: router,
	}

	go func() {
		log.Printf("panel listening on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func nodeClientResolver(pool *pgxpool.Pool, encryptionKey string) func(nodeID int64) (*daemonclient.Client, error) {
	return func(nodeID int64) (*daemonclient.Client, error) {
		var (
			scheme         string
			fqdn           string
			daemonPort     int
			tokenEncrypted *string
		)
		err := pool.QueryRow(context.Background(),
			`SELECT scheme, fqdn, daemon_port, daemon_token_encrypted FROM nodes WHERE id = $1`, nodeID,
		).Scan(&scheme, &fqdn, &daemonPort, &tokenEncrypted)
		if err != nil {
			return nil, fmt.Errorf("look up node %d: %w", nodeID, err)
		}
		if tokenEncrypted == nil || *tokenEncrypted == "" {
			return nil, fmt.Errorf("node %d has no stored daemon token — it must be re-created to pick up token storage", nodeID)
		}

		token, err := crypto.Decrypt(encryptionKey, *tokenEncrypted)
		if err != nil {
			return nil, fmt.Errorf("decrypt daemon token for node %d: %w", nodeID, err)
		}

		baseURL := fmt.Sprintf("%s://%s:%d", scheme, fqdn, daemonPort)
		return daemonclient.New(baseURL, token), nil
	}
}
