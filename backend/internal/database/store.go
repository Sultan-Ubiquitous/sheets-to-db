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
	// added last_updated_by to query
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
		var discount bool // Changed to bool based on your schema (TINYINT(1))

		// Scan lastUpdatedBy as well
		if err := rows.Scan(&uuid, &name, &qty, &price, &discount, &lastUpdatedBy); err != nil {
			return nil, err
		}

		products = append(products, map[string]interface{}{
			"uuid":            uuid,
			"product_name":    name,
			"quantity":        qty,
			"price":           price,
			"discount":        discount,
			"last_updated_by": lastUpdatedBy, // Added field
		})
	}
	return products, nil
}

// NEW FUNCTION: Fetch single product with all fields
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

func UpdateProductField(uuid string, dbField string, value interface{}, userEmail string) error {
	// 1. Safety Guard: Whitelist allowed columns to prevent SQL injection
	// (even though the handler maps them, this is a second line of defense)
	allowedFields := map[string]bool{
		"product_name": true,
		"quantity":     true,
		"price":        true,
		"discount":     true,
	}

	if !allowedFields[dbField] {
		return fmt.Errorf("invalid database field: %s", dbField)
	}

	// 2. Construct Query
	// Note: We inject dbField directly because we validated it against the whitelist above.
	query := fmt.Sprintf("UPDATE product SET %s = ?, last_updated_by = ?, updated_at = ? WHERE uuid = ?", dbField)

	// 3. Execute
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

	// LOGIC:
	// We construct a specific query for each scenario.
	// If we are inserting a "Price", we MUST provide a dummy "Product Name" to satisfy the NOT NULL constraint.
	// If we are inserting a "Name", we MUST provide a dummy "Price".
	// If we are inserting "Quantity", we MUST provide BOTH.

	switch dbField {
	case "product_name":
		// User provided Name, we inject dummy Price (0.00) for the INSERT case
		query = `
			INSERT INTO product (uuid, product_name, price, last_updated_by, updated_at) 
			VALUES (?, ?, 0.00, ?, ?) 
			ON DUPLICATE KEY UPDATE 
				product_name = VALUES(product_name), 
				last_updated_by = VALUES(last_updated_by), 
				updated_at = VALUES(updated_at)
		`

	case "price":
		// User provided Price, we inject dummy Name ('New Product') for the INSERT case
		query = `
			INSERT INTO product (uuid, price, product_name, last_updated_by, updated_at) 
			VALUES (?, ?, 'New Product', ?, ?) 
			ON DUPLICATE KEY UPDATE 
				price = VALUES(price), 
				last_updated_by = VALUES(last_updated_by), 
				updated_at = VALUES(updated_at)
		`

	default:
		// User provided Quantity or Discount. We inject BOTH dummy Name and Price.
		// Note: We safely inject dbField here because it's whitelisted above.
		query = fmt.Sprintf(`
			INSERT INTO product (uuid, %s, product_name, price, last_updated_by, updated_at) 
			VALUES (?, ?, 'New Product', 0.00, ?, ?) 
			ON DUPLICATE KEY UPDATE 
				%s = VALUES(%s), 
				last_updated_by = VALUES(last_updated_by), 
				updated_at = VALUES(updated_at)
		`, dbField, dbField, dbField)
	}

	// Execute with the standard arguments (UUID, Value, Email, Time)
	// The dummy values ('New Product', 0.00) are hardcoded in the SQL string above.
	_, err := tx.Exec(query, uuid, value, userEmail, time.Now())

	return err
}
