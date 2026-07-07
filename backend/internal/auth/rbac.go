package auth

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	PermControlStart   = "control.start"
	PermControlStop    = "control.stop"
	PermControlRestart = "control.restart"
	PermControlKill    = "control.kill"
	PermConsole        = "console"
	PermFilesRead      = "files.read"
	PermFilesWrite     = "files.write"
	PermSchedulesRead  = "schedules.read"
	PermSchedulesWrite = "schedules.write"
	PermDatabasesRead  = "databases.read"
	PermDatabasesWrite = "databases.write"
	PermDomainsRead    = "domains.read"
	PermDomainsWrite   = "domains.write"
	PermBackupsRead    = "backups.read"
	PermBackupsWrite   = "backups.write"

	PermServersRead  = "servers.read"
	PermServersWrite = "servers.write"
)

type PermissionChecker interface {
	HasPermission(ctx context.Context, userID int64, code string) (bool, error)
	HasServerPermission(ctx context.Context, userID, serverID int64, code string) (bool, error)
}

type SubuserChecker struct {
	DB *pgxpool.Pool
}

func NewSubuserChecker(db *pgxpool.Pool) *SubuserChecker {
	return &SubuserChecker{DB: db}
}

func (c *SubuserChecker) HasPermission(ctx context.Context, userID int64, code string) (bool, error) {
	return false, nil
}

func (c *SubuserChecker) HasServerPermission(ctx context.Context, userID, serverID int64, code string) (bool, error) {
	var raw []byte
	if err := c.DB.QueryRow(ctx,
		`SELECT permissions FROM server_subusers WHERE server_id = $1 AND user_id = $2`,
		serverID, userID,
	).Scan(&raw); err != nil {
		return false, nil
	}
	var perms []string
	if err := json.Unmarshal(raw, &perms); err != nil {
		return false, nil
	}
	for _, p := range perms {
		if p == code {
			return true, nil
		}
	}
	return false, nil
}

func (c *SubuserChecker) IsSubuser(ctx context.Context, userID, serverID int64) (bool, error) {
	var exists bool
	err := c.DB.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM server_subusers WHERE server_id = $1 AND user_id = $2)`,
		serverID, userID,
	).Scan(&exists)
	return exists, err
}

func (c *SubuserChecker) CanAccessServer(ctx context.Context, claims *Claims, ownerID, serverID int64, permission string) bool {
	if claims.IsAdmin || claims.UserID == ownerID {
		return true
	}
	allowed, _ := c.HasServerPermission(ctx, claims.UserID, serverID, permission)
	return allowed
}

func (c *SubuserChecker) CanViewServer(ctx context.Context, claims *Claims, ownerID, serverID int64) bool {
	if claims.IsAdmin || claims.UserID == ownerID {
		return true
	}
	isSub, _ := c.IsSubuser(ctx, claims.UserID, serverID)
	return isSub
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := FromContext(r.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !claims.IsAdmin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func RequirePermission(checker PermissionChecker, code string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := FromContext(r.Context())
			if !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if claims.IsAdmin {
				next.ServeHTTP(w, r)
				return
			}
			allowed, err := checker.HasPermission(r.Context(), claims.UserID, code)
			if err != nil {
				http.Error(w, "permission check failed", http.StatusInternalServerError)
				return
			}
			if !allowed {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
