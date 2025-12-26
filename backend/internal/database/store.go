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

func SaveSheetID(name, spreadsheetID string) error {
	query := `
        INSERT INTO sheet_mappings (name, spreadsheet_id) 
        VALUES (?, ?) 
        ON DUPLICATE KEY UPDATE spreadsheet_id = VALUES(spreadsheet_id)
    `
	_, err := DB.Exec(query, name, spreadsheetID)
	return err
}

func GetAllProducts() ([]map[string]interface{}, error) {
	rows, err := DB.Query("SELECT uuid, product_name, quantity, price, discount FROM product")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []map[string]interface{}
	for rows.Next() {
		var uuid, name string
		var qty int
		var price, discount float64

		if err := rows.Scan(&uuid, &name, &qty, &price, &discount); err != nil {
			return nil, err
		}

		products = append(products, map[string]interface{}{
			"uuid":         uuid,
			"product_name": name,
			"quantity":     qty,
			"price":        price,
			"discount":     discount,
		})
	}
	return products, nil
}

// 2. Get the CURRENT Binlog position so we know where to start listening
func GetMasterStatus() (string, uint32, error) {
	var file string
	var position uint32
	var binlogDoDB, binlogIgnoreDB, executedGtidSet interface{} // Ignore these columns

	// MySQL command to get current position
	row := DB.QueryRow("SHOW MASTER STATUS")

	// Scan structure depends on MySQL version, but usually File, Position are first 2
	err := row.Scan(&file, &position, &binlogDoDB, &binlogIgnoreDB, &executedGtidSet)
	if err != nil {
		// Handle cases where fewer columns are returned (older MySQL)
		err = row.Scan(&file, &position, &binlogDoDB, &binlogIgnoreDB)
		if err != nil {
			return "", 0, fmt.Errorf("failed to get master status: %v", err)
		}
	}
	return file, position, nil
}

func GetSheetID(name string) (string, error) {
	var sheetID string
	query := "SELECT spreadsheet_id FROM sheet_mappings WHERE name = ?"
	err := DB.QueryRow(query, name).Scan(&sheetID)
	if err != nil {
		return "", err
	}
	return sheetID, nil
}

func GetLatestToken() (*oauth2.Token, error) {
	var accessToken, refreshToken, tokenType string
	var expiry time.Time

	query := `
			  SELECT access_token, refresh_token, token_type, expiry 
              FROM oauth_tokens 
              ORDER BY updated_at DESC LIMIT 1
	`

	err := DB.QueryRow(query).Scan(&accessToken, &refreshToken, &tokenType, &expiry)
	if err != nil {
		return nil, err
	}

	return &oauth2.Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    tokenType,
		Expiry:       expiry,
	}, nil
}
