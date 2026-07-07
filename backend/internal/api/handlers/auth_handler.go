package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/panel/internal/activity"
	"github.com/yourorg/panel/internal/auth"
	"github.com/yourorg/panel/internal/crypto"
	"github.com/yourorg/panel/internal/ratelimit"
)

type AuthHandler struct {
	DB            *pgxpool.Pool
	Token         *auth.TokenManager
	EncryptionKey string
	Limiter       *ratelimit.Limiter
}

const (
	loginRateLimit  = 10
	loginRateWindow = 15 * time.Minute
)

func (h *AuthHandler) checkLoginRateLimit(w http.ResponseWriter, r *http.Request) bool {
	allowed, err := h.Limiter.Allow(r.Context(), "ratelimit:login:"+activity.RequestIP(r), loginRateLimit, loginRateWindow)
	if err != nil {
		log.Printf("ratelimit: login check failed, allowing request: %v", err)
		return true
	}
	if !allowed {
		http.Error(w, "too many login attempts — try again later", http.StatusTooManyRequests)
		return false
	}
	return true
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TOTPCode string `json:"totp_code"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	User         struct {
		ID       int64  `json:"id"`
		Email    string `json:"email"`
		Username string `json:"username"`
	} `json:"user"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if !h.checkLoginRateLimit(w, r) {
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var (
		id            int64
		email         string
		username      string
		passwordHash  string
		isAdmin       bool
		isActive      bool
		totpEnabled   bool
		totpSecretEnc *string
	)
	err := h.DB.QueryRow(r.Context(),
		`SELECT id, email, username, password_hash, is_admin, is_active, totp_enabled, totp_secret
		 FROM users WHERE email = $1`, req.Email,
	).Scan(&id, &email, &username, &passwordHash, &isAdmin, &isActive, &totpEnabled, &totpSecretEnc)

	if err == pgx.ErrNoRows {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	} else if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if !isActive || !auth.VerifyPassword(passwordHash, req.Password) {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	if totpEnabled {
		if req.TOTPCode == "" {
			http.Error(w, "totp_required", http.StatusPreconditionRequired)
			return
		}
		secret, err := crypto.Decrypt(h.EncryptionKey, *totpSecretEnc)
		if err != nil || !auth.ValidateTOTPCode(secret, req.TOTPCode) {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
	}

	accessToken, err := h.Token.Issue(id, email, isAdmin)
	if err != nil {
		http.Error(w, "failed to issue token", http.StatusInternalServerError)
		return
	}
	refreshToken, err := h.Token.IssueRefresh(id, email, isAdmin)
	if err != nil {
		http.Error(w, "failed to issue token", http.StatusInternalServerError)
		return
	}

	activity.Record(r.Context(), h.DB, activity.Entry{
		ActorUserID: &id,
		Event:       "user.login",
		IPAddress:   activity.RequestIP(r),
	})

	resp := tokenResponse{AccessToken: accessToken, RefreshToken: refreshToken}
	resp.User.ID = id
	resp.User.Email = email
	resp.User.Username = username

	writeJSON(w, http.StatusOK, resp)
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	claims, err := h.Token.ParseRefresh(req.RefreshToken)
	if err != nil {
		http.Error(w, "invalid or expired refresh token", http.StatusUnauthorized)
		return
	}

	var (
		email    string
		username string
		isAdmin  bool
		isActive bool
	)
	err = h.DB.QueryRow(r.Context(),
		`SELECT email, username, is_admin, is_active FROM users WHERE id = $1`, claims.UserID,
	).Scan(&email, &username, &isAdmin, &isActive)
	if err != nil || !isActive {
		http.Error(w, "account no longer active", http.StatusUnauthorized)
		return
	}

	accessToken, err := h.Token.Issue(claims.UserID, email, isAdmin)
	if err != nil {
		http.Error(w, "failed to issue token", http.StatusInternalServerError)
		return
	}
	refreshToken, err := h.Token.IssueRefresh(claims.UserID, email, isAdmin)
	if err != nil {
		http.Error(w, "failed to issue token", http.StatusInternalServerError)
		return
	}

	resp := tokenResponse{AccessToken: accessToken, RefreshToken: refreshToken}
	resp.User.ID = claims.UserID
	resp.User.Email = email
	resp.User.Username = username

	writeJSON(w, http.StatusOK, resp)
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       claims.UserID,
		"email":    claims.Email,
		"is_admin": claims.IsAdmin,
	})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.NewPassword) < 8 {
		http.Error(w, "new password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	var passwordHash string
	if err := h.DB.QueryRow(r.Context(),
		`SELECT password_hash FROM users WHERE id = $1`, claims.UserID,
	).Scan(&passwordHash); err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if !auth.VerifyPassword(passwordHash, req.CurrentPassword) {
		http.Error(w, "current password is incorrect", http.StatusForbidden)
		return
	}

	newHash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		http.Error(w, "failed to hash password", http.StatusInternalServerError)
		return
	}
	if _, err := h.DB.Exec(r.Context(),
		`UPDATE users SET password_hash = $1, updated_at = now() WHERE id = $2`, newHash, claims.UserID,
	); err != nil {
		http.Error(w, "failed to update password", http.StatusInternalServerError)
		return
	}

	activity.Record(r.Context(), h.DB, activity.Entry{
		ActorUserID: &claims.UserID,
		Event:       "account.change_password",
		IPAddress:   activity.RequestIP(r),
	})

	w.WriteHeader(http.StatusNoContent)
}
