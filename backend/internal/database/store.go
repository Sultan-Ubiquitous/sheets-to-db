package database

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/oauth2"
)

var DB *sql.DB

func InitDB(dsn string) error {
	var err error
	DB, err = sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	return DB.Ping()
}

func UpsertToken(email string, token *oauth2.Token) error {
	query := `
		INSERT INTO oauth_tokens (user_email, access_token, refresh_token, token_type, expiry)
		VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			access_token = VALUES(access_token),
			refresh_token = IF(VALUES(refresh_token) != '', VALUES(refresh_token), refresh_token),
			token_type = VALUES(token_type),
			expiry = VALUES(expiry),
			updated_at = CURRENT_TIMESTAMP
	`

	expiry := token.Expiry
	if expiry.IsZero() {
		expiry = time.Now().Add(1 * time.Hour)
	}

	_, err := DB.Exec(query,
		email,
		token.AccessToken,
		token.RefreshToken,
		token.TokenType,
		expiry,
	)

	if err != nil {
		return fmt.Errorf("failed to store token: %w", err)
	}
	return nil
}
