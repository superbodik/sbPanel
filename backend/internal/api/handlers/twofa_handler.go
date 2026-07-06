package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/panel/internal/activity"
	"github.com/yourorg/panel/internal/auth"
	"github.com/yourorg/panel/internal/crypto"
	"github.com/yourorg/panel/internal/ratelimit"
)

type TwoFAHandler struct {
	DB            *pgxpool.Pool
	EncryptionKey string
	Limiter       *ratelimit.Limiter
}

const (
	twofaVerifyRateLimit  = 10
	twofaVerifyRateWindow = 15 * time.Minute
)

func (h *TwoFAHandler) Status(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var enabled bool
	if err := h.DB.QueryRow(r.Context(),
		`SELECT totp_enabled FROM users WHERE id = $1`, claims.UserID,
	).Scan(&enabled); err != nil {
		http.Error(w, "failed to load status", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"enabled": enabled})
}

func (h *TwoFAHandler) Setup(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	secret, otpauthURL, err := auth.GenerateTOTPSecret("Roost", claims.Email)
	if err != nil {
		http.Error(w, "failed to generate secret", http.StatusInternalServerError)
		return
	}

	encrypted, err := crypto.Encrypt(h.EncryptionKey, secret)
	if err != nil {
		http.Error(w, "failed to encrypt secret", http.StatusInternalServerError)
		return
	}

	if _, err := h.DB.Exec(r.Context(),
		`UPDATE users SET totp_secret = $1, totp_enabled = false WHERE id = $2`,
		encrypted, claims.UserID,
	); err != nil {
		http.Error(w, "failed to store secret", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"secret": secret, "otpauth_url": otpauthURL})
}

type twofaCodeRequest struct {
	Code string `json:"code"`
}

func (h *TwoFAHandler) Verify(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	allowed, err := h.Limiter.Allow(r.Context(),
		"ratelimit:2fa-verify:"+strconv.FormatInt(claims.UserID, 10), twofaVerifyRateLimit, twofaVerifyRateWindow)
	if err != nil {
		log.Printf("ratelimit: 2fa verify check failed, allowing request: %v", err)
	} else if !allowed {
		http.Error(w, "too many attempts — try again later", http.StatusTooManyRequests)
		return
	}

	var req twofaCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var encryptedSecret *string
	if err := h.DB.QueryRow(r.Context(),
		`SELECT totp_secret FROM users WHERE id = $1`, claims.UserID,
	).Scan(&encryptedSecret); err != nil || encryptedSecret == nil {
		http.Error(w, "no pending 2FA setup — call setup first", http.StatusBadRequest)
		return
	}

	secret, err := crypto.Decrypt(h.EncryptionKey, *encryptedSecret)
	if err != nil || !auth.ValidateTOTPCode(secret, req.Code) {
		http.Error(w, "invalid code", http.StatusUnauthorized)
		return
	}

	if _, err := h.DB.Exec(r.Context(),
		`UPDATE users SET totp_enabled = true WHERE id = $1`, claims.UserID,
	); err != nil {
		http.Error(w, "failed to enable 2FA", http.StatusInternalServerError)
		return
	}

	activity.Record(r.Context(), h.DB, activity.Entry{
		ActorUserID: &claims.UserID,
		Event:       "user.2fa.enable",
		IPAddress:   activity.RequestIP(r),
	})

	writeJSON(w, http.StatusOK, map[string]any{"enabled": true})
}

type twofaDisableRequest struct {
	Password string `json:"password"`
}

func (h *TwoFAHandler) Disable(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req twofaDisableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var passwordHash string
	if err := h.DB.QueryRow(r.Context(),
		`SELECT password_hash FROM users WHERE id = $1`, claims.UserID,
	).Scan(&passwordHash); err != nil {
		http.Error(w, "user not found", http.StatusInternalServerError)
		return
	}
	if !auth.VerifyPassword(passwordHash, req.Password) {
		http.Error(w, "invalid password", http.StatusUnauthorized)
		return
	}

	if _, err := h.DB.Exec(r.Context(),
		`UPDATE users SET totp_enabled = false, totp_secret = NULL WHERE id = $1`, claims.UserID,
	); err != nil {
		http.Error(w, "failed to disable 2FA", http.StatusInternalServerError)
		return
	}

	activity.Record(r.Context(), h.DB, activity.Entry{
		ActorUserID: &claims.UserID,
		Event:       "user.2fa.disable",
		IPAddress:   activity.RequestIP(r),
	})

	writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
}
