package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/Sultan-Ubiquitous/sheets-to-db/database"
)

// --- MISSING STRUCT ADDED BACK HERE ---
type SheetUpdatePayload struct {
	UUID      string      `json:"uuid"`
	Field     string      `json:"field"`
	Value     interface{} `json:"value"`
	UserEmail string      `json:"user_email"`
}

// Reusable value parser
func parseValue(field string, val interface{}) (string, interface{}) {
	var dbValue interface{}
	var dbField string

	switch field {
	case "Product Name":
		dbField = "product_name"
		dbValue = fmt.Sprintf("%v", val)
	case "Quantity":
		dbField = "quantity"
		if v, ok := val.(float64); ok {
			dbValue = int(v)
		} else {
			i, _ := strconv.Atoi(fmt.Sprintf("%v", val))
			dbValue = i
		}
	case "Price":
		dbField = "price"
		if v, ok := val.(float64); ok {
			dbValue = v
		} else {
			f, _ := strconv.ParseFloat(fmt.Sprintf("%v", val), 64)
			dbValue = f
		}
	case "Discount":
		dbField = "discount"
		if v, ok := val.(bool); ok {
			dbValue = v
		} else {
			strVal := fmt.Sprintf("%v", val)
			dbValue = (strVal == "true" || strVal == "TRUE")
		}
	default:
		return "", nil // Signal to skip
	}
	return dbField, dbValue
}

func SheetWebhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Read the body bytes so we can try decoding multiple times if needed
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	// r.Body is now drained, so we use bodyBytes

	var payloads []SheetUpdatePayload

	// 2. Try Decoding as an ARRAY (The new standard)
	if err := json.Unmarshal(bodyBytes, &payloads); err != nil {
		// 3. Fallback: If Array fails, try decoding as a SINGLE OBJECT
		var singlePayload SheetUpdatePayload
		if err := json.Unmarshal(bodyBytes, &singlePayload); err == nil {
			// Success! It was a single object. Wrap it in a slice.
			payloads = []SheetUpdatePayload{singlePayload}
		} else {
			// Both failed -> Actual invalid JSON
			http.Error(w, "Invalid JSON format", http.StatusBadRequest)
			log.Printf("Webhook Decode Error: %v", err)
			return
		}
	}

	if len(payloads) == 0 {
		w.WriteHeader(http.StatusOK)
		return
	}

	log.Printf("Received Update with %d changes", len(payloads))

	// ... (The rest of your transaction logic remains exactly the same) ...

	successCount := 0
	tx, err := database.DB.Begin()
	if err != nil {
		http.Error(w, "DB Error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	for _, p := range payloads {
		if p.UUID == "" || p.Field == "" {
			continue
		}

		dbField, dbValue := parseValue(p.Field, p.Value)
		if dbField == "" {
			continue
		}

		if err := database.TxUpsertProductField(tx, p.UUID, dbField, dbValue, p.UserEmail); err != nil {
			log.Printf("Batch item failed (%s): %v", p.UUID, err)
		} else {
			successCount++
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Transaction Commit Failed: %v", err)
		http.Error(w, "Transaction failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("Processed %d updates", successCount)))
}
