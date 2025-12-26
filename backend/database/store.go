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
	rows, err := DB.Query("SELECT uuid, product_name, quantity, price, discount, last_updated_by FROM product")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []map[string]interface{}
	for rows.Next() {
		var uuid, name, lastUpdatedBy string
		var qty int
		var price float64
		var discount bool

		if err := rows.Scan(&uuid, &name, &qty, &price, &discount, &lastUpdatedBy); err != nil {
			return nil, err
		}

		products = append(products, map[string]interface{}{
			"uuid":            uuid,
			"product_name":    name,
			"quantity":        qty,
			"price":           price,
			"discount":        discount,
			"last_updated_by": lastUpdatedBy,
		})
	}
	return products, nil
}

func GetProductByUUID(uuid string) (map[string]interface{}, error) {
	query := `
        SELECT uuid, product_name, quantity, price, discount, last_updated_by 
        FROM product WHERE uuid = ?
    `

	var name, lastUpdatedBy string
	var qty int
	var price float64
	var discount bool

	err := DB.QueryRow(query, uuid).Scan(&uuid, &name, &qty, &price, &discount, &lastUpdatedBy)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("product not found")
		}
		return nil, err
	}

	return map[string]interface{}{
		"uuid":            uuid,
		"product_name":    name,
		"quantity":        qty,
		"price":           price,
		"discount":        discount,
		"last_updated_by": lastUpdatedBy,
	}, nil
}

func GetMasterStatus() (string, uint32, error) {
	var file string
	var position uint32
	var binlogDoDB, binlogIgnoreDB, executedGtidSet interface{}

	row := DB.QueryRow("SHOW MASTER STATUS")

	err := row.Scan(&file, &position, &binlogDoDB, &binlogIgnoreDB, &executedGtidSet)
	if err != nil {
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

func UpdateProductField(uuid string, dbField string, value interface{}, userEmail string) error {

	allowedFields := map[string]bool{
		"product_name": true,
		"quantity":     true,
		"price":        true,
		"discount":     true,
	}

	if !allowedFields[dbField] {
		return fmt.Errorf("invalid database field: %s", dbField)
	}

	query := fmt.Sprintf("UPDATE product SET %s = ?, last_updated_by = ?, updated_at = ? WHERE uuid = ?", dbField)

	_, err := DB.Exec(query, value, userEmail, time.Now(), uuid)
	return err
}

func TxUpsertProductField(tx *sql.Tx, uuid string, dbField string, value interface{}, userEmail string) error {
	allowedFields := map[string]bool{
		"product_name": true,
		"quantity":     true,
		"price":        true,
		"discount":     true,
	}

	if !allowedFields[dbField] {
		return fmt.Errorf("invalid database field: %s", dbField)
	}

	var query string

	switch dbField {
	case "product_name":
		query = `
			INSERT INTO product (uuid, product_name, price, last_updated_by, updated_at) 
			VALUES (?, ?, 0.00, ?, ?) 
			ON DUPLICATE KEY UPDATE 
				product_name = VALUES(product_name), 
				last_updated_by = VALUES(last_updated_by), 
				updated_at = VALUES(updated_at)
		`

	case "price":
		query = `
			INSERT INTO product (uuid, price, product_name, last_updated_by, updated_at) 
			VALUES (?, ?, 'New Product', ?, ?) 
			ON DUPLICATE KEY UPDATE 
				price = VALUES(price), 
				last_updated_by = VALUES(last_updated_by), 
				updated_at = VALUES(updated_at)
		`

	default:
		query = fmt.Sprintf(`
			INSERT INTO product (uuid, %s, product_name, price, last_updated_by, updated_at) 
			VALUES (?, ?, 'New Product', 0.00, ?, ?) 
			ON DUPLICATE KEY UPDATE 
				%s = VALUES(%s), 
				last_updated_by = VALUES(last_updated_by), 
				updated_at = VALUES(updated_at)
		`, dbField, dbField, dbField)
	}
	_, err := tx.Exec(query, uuid, value, userEmail, time.Now())

	return err
}
