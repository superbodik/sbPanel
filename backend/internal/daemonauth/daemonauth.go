package daemonauth

import (
	"context"
	"crypto/subtle"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/panel/internal/crypto"
)

func VerifyNodeToken(ctx context.Context, db *pgxpool.Pool, encryptionKey string, nodeID int64, presented string) bool {
	if presented == "" {
		return false
	}

	var tokenEncrypted *string
	if err := db.QueryRow(ctx,
		`SELECT daemon_token_encrypted FROM nodes WHERE id = $1`, nodeID,
	).Scan(&tokenEncrypted); err != nil || tokenEncrypted == nil {
		return false
	}

	expected, err := crypto.Decrypt(encryptionKey, *tokenEncrypted)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(expected)) == 1
}
