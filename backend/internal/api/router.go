package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/panel/internal/api/handlers"
	"github.com/yourorg/panel/internal/auth"
	"github.com/yourorg/panel/internal/daemonclient"
	"github.com/yourorg/panel/internal/ratelimit"
	"github.com/yourorg/panel/internal/ws"
)

type Dependencies struct {
	DB            *pgxpool.Pool
	Token         *auth.TokenManager
	Hub           *ws.Hub
	NodeClient    func(nodeID int64) (*daemonclient.Client, error)
	EncryptionKey string
	Limiter       *ratelimit.Limiter
	Version       string
	Commit        string
	BuildDate     string
	SourceDir     string
	RepoSlug      string
}

const maxRequestBodyBytes = 100 << 20

func maxBodySize(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(maxBodySize(maxRequestBodyBytes))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: true,
	}))

	subusers := auth.NewSubuserChecker(deps.DB)

	authHandler := &handlers.AuthHandler{DB: deps.DB, Token: deps.Token, EncryptionKey: deps.EncryptionKey, Limiter: deps.Limiter}
	nodeHandler := &handlers.NodeHandler{DB: deps.DB, EncryptionKey: deps.EncryptionKey, NodeClient: deps.NodeClient}
	serverHandler := &handlers.ServerHandler{DB: deps.DB, NodeClient: deps.NodeClient, Subusers: subusers}
	versionHandler := &handlers.VersionHandler{
		Version:   deps.Version,
		Commit:    deps.Commit,
		BuildDate: deps.BuildDate,
		SourceDir: deps.SourceDir,
		RepoSlug:  deps.RepoSlug,
	}
	activityHandler := &handlers.ActivityHandler{DB: deps.DB}
	eggHandler := &handlers.EggHandler{DB: deps.DB}
	allocationHandler := &handlers.AllocationHandler{DB: deps.DB}
	apiKeyHandler := &handlers.APIKeyHandler{DB: deps.DB}
	fileHandler := &handlers.FileHandler{DB: deps.DB, NodeClient: deps.NodeClient, Subusers: subusers}
	scheduleHandler := &handlers.ScheduleHandler{DB: deps.DB, Subusers: subusers}
	twofaHandler := &handlers.TwoFAHandler{DB: deps.DB, EncryptionKey: deps.EncryptionKey, Limiter: deps.Limiter}
	subuserHandler := &handlers.SubuserHandler{DB: deps.DB}
	userHandler := &handlers.UserHandler{DB: deps.DB}
	databaseHostHandler := &handlers.DatabaseHostHandler{DB: deps.DB, EncryptionKey: deps.EncryptionKey}
	serverDatabaseHandler := &handlers.ServerDatabaseHandler{DB: deps.DB, Subusers: subusers, Encrypt: deps.EncryptionKey}
	serverDomainHandler := &handlers.ServerDomainHandler{DB: deps.DB, Subusers: subusers, NodeClient: deps.NodeClient}
	serverBackupHandler := &handlers.ServerBackupHandler{DB: deps.DB, Subusers: subusers, NodeClient: deps.NodeClient}
	sshKeyHandler := &handlers.SSHKeyHandler{DB: deps.DB}
	sftpAuthHandler := &handlers.SFTPAuthHandler{DB: deps.DB, Subusers: subusers, EncryptionKey: deps.EncryptionKey}

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", authHandler.Login)
		r.Post("/auth/refresh", authHandler.Refresh)
		r.Post("/internal/sftp/authenticate", sftpAuthHandler.Authenticate)

		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(30 * time.Second))
			r.Use(auth.Middleware(deps.Token, resolveAPIKey(deps.DB)))

			r.Get("/auth/me", authHandler.Me)

			r.Get("/nodes", nodeHandler.List)
			r.With(auth.RequireAdmin).Post("/nodes", nodeHandler.Create)
			r.With(auth.RequireAdmin).Patch("/nodes/{id}", nodeHandler.Update)
			r.With(auth.RequireAdmin).Post("/nodes/{id}/regenerate-token", nodeHandler.RegenerateToken)
			r.With(auth.RequireAdmin).Delete("/nodes/{id}", nodeHandler.Delete)
			r.Get("/nodes/{id}/status", nodeHandler.Status)

			r.With(auth.RequireAdmin).Get("/users", userHandler.List)
			r.With(auth.RequireAdmin).Patch("/users/{id}", userHandler.Update)

			r.Get("/database-hosts", databaseHostHandler.List)
			r.With(auth.RequireAdmin).Post("/database-hosts", databaseHostHandler.Create)
			r.With(auth.RequireAdmin).Delete("/database-hosts/{id}", databaseHostHandler.Delete)

			r.Get("/servers", serverHandler.List)
			r.Post("/servers", serverHandler.Create)
			r.Get("/servers/{uuid}", serverHandler.Get)
			r.Post("/servers/{uuid}/power", serverHandler.Power)
			r.Delete("/servers/{uuid}", serverHandler.Delete)

			r.Get("/servers/{uuid}/files", fileHandler.List)
			r.Get("/servers/{uuid}/files/contents", fileHandler.Read)
			r.Put("/servers/{uuid}/files/contents", fileHandler.Write)
			r.Delete("/servers/{uuid}/files", fileHandler.Delete)
			r.Post("/servers/{uuid}/files/directory", fileHandler.CreateDirectory)
			r.Post("/servers/{uuid}/files/rename", fileHandler.Rename)

			r.Get("/servers/{uuid}/schedules", scheduleHandler.List)
			r.Post("/servers/{uuid}/schedules", scheduleHandler.Create)
			r.Post("/servers/{uuid}/schedules/{id}/toggle", scheduleHandler.Toggle)
			r.Delete("/servers/{uuid}/schedules/{id}", scheduleHandler.Delete)

			r.Get("/servers/{uuid}/databases", serverDatabaseHandler.List)
			r.Post("/servers/{uuid}/databases", serverDatabaseHandler.Create)
			r.Delete("/servers/{uuid}/databases/{id}", serverDatabaseHandler.Delete)

			r.Get("/servers/{uuid}/domains", serverDomainHandler.List)

			r.Get("/servers/{uuid}/backups", serverBackupHandler.List)
			r.Delete("/servers/{uuid}/backups/{id}", serverBackupHandler.Delete)

			r.Get("/servers/{uuid}/subusers", subuserHandler.List)
			r.Post("/servers/{uuid}/subusers", subuserHandler.Create)
			r.Patch("/servers/{uuid}/subusers/{id}", subuserHandler.Update)
			r.Delete("/servers/{uuid}/subusers/{id}", subuserHandler.Delete)

			r.Get("/eggs", eggHandler.List)

			r.Get("/allocations", allocationHandler.List)
			r.With(auth.RequireAdmin).Post("/allocations", allocationHandler.Create)
			r.With(auth.RequireAdmin).Delete("/allocations/{id}", allocationHandler.Delete)

			r.Get("/version", versionHandler.Get)
			r.Get("/version/check", versionHandler.CheckUpdate)

			r.With(auth.RequireAdmin).Get("/activity", activityHandler.List)

			r.Get("/account/api-keys", apiKeyHandler.List)
			r.Post("/account/api-keys", apiKeyHandler.Create)
			r.Delete("/account/api-keys/{id}", apiKeyHandler.Delete)

			r.Get("/account/ssh-keys", sshKeyHandler.List)
			r.Post("/account/ssh-keys", sshKeyHandler.Create)
			r.Delete("/account/ssh-keys/{id}", sshKeyHandler.Delete)

			r.Get("/account/2fa/status", twofaHandler.Status)
			r.Post("/account/2fa/setup", twofaHandler.Setup)
			r.Post("/account/2fa/verify", twofaHandler.Verify)
			r.Post("/account/2fa/disable", twofaHandler.Disable)
		})

		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(150 * time.Second))
			r.Use(auth.Middleware(deps.Token, resolveAPIKey(deps.DB)))

			r.Post("/servers/{uuid}/domains", serverDomainHandler.Create)
			r.Delete("/servers/{uuid}/domains/{id}", serverDomainHandler.Delete)

			r.Post("/servers/{uuid}/backups", serverBackupHandler.Create)
			r.Post("/servers/{uuid}/backups/{id}/restore", serverBackupHandler.Restore)
			r.Get("/servers/{uuid}/backups/{id}/download", serverBackupHandler.Download)
		})
	})

	r.Get("/ws/servers/{uuid}", func(w http.ResponseWriter, r *http.Request) {
		id, err := parseUUIDParam(r, "uuid")
		if err != nil {
			http.Error(w, "invalid server uuid", http.StatusBadRequest)
			return
		}
		claims, ok := authenticateWS(r, deps.Token)
		if !ok || !canAccessServerWS(r, deps.DB, subusers, claims, id, "") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		deps.Hub.ServeServerSocket(w, r, id)
	})

	r.Get("/ws/servers/{uuid}/console", func(w http.ResponseWriter, r *http.Request) {
		id, err := parseUUIDParam(r, "uuid")
		if err != nil {
			http.Error(w, "invalid server uuid", http.StatusBadRequest)
			return
		}
		claims, ok := authenticateWS(r, deps.Token)
		if !ok || !canAccessServerWS(r, deps.DB, subusers, claims, id, auth.PermConsole) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		deps.Hub.ServeConsoleSocket(w, r, id)
	})

	return r
}

func authenticateWS(r *http.Request, tm *auth.TokenManager) (*auth.Claims, bool) {
	token := r.URL.Query().Get("token")
	if token == "" {
		return nil, false
	}
	claims, err := tm.Parse(token)
	if err != nil || claims.Type != auth.TokenAccess {
		return nil, false
	}
	return claims, true
}

func canAccessServerWS(r *http.Request, db *pgxpool.Pool, subusers *auth.SubuserChecker, claims *auth.Claims, serverUUID uuid.UUID, permission string) bool {
	var serverID, ownerID int64
	if err := db.QueryRow(r.Context(),
		`SELECT id, owner_id FROM servers WHERE uuid = $1`, serverUUID,
	).Scan(&serverID, &ownerID); err != nil {
		return false
	}
	if permission == "" {
		return subusers.CanViewServer(r.Context(), claims, ownerID, serverID)
	}
	return subusers.CanAccessServer(r.Context(), claims, ownerID, serverID, permission)
}

func resolveAPIKey(pool *pgxpool.Pool) auth.APIKeyResolver {
	return func(ctx context.Context, rawToken string) (*auth.Claims, error) {
		sum := sha256.Sum256([]byte(rawToken))
		tokenHash := hex.EncodeToString(sum[:])

		var userID int64
		var email string
		var isAdmin, isActive bool
		var permsRaw []byte
		err := pool.QueryRow(ctx, `
			SELECT u.id, u.email, u.is_admin, u.is_active, k.permissions
			FROM api_keys k JOIN users u ON u.id = k.user_id
			WHERE k.token_hash = $1`, tokenHash,
		).Scan(&userID, &email, &isAdmin, &isActive, &permsRaw)
		if err != nil || !isActive {
			return nil, auth.ErrInvalidToken
		}

		_, _ = pool.Exec(ctx, `UPDATE api_keys SET last_used_at = now() WHERE token_hash = $1`, tokenHash)

		claims := &auth.Claims{UserID: userID, Email: email, IsAdmin: isAdmin, Type: auth.TokenAccess}
		var perms []string
		if json.Unmarshal(permsRaw, &perms) == nil && len(perms) > 0 {
			claims.KeyPermissions = &perms
		}
		return claims, nil
	}
}
