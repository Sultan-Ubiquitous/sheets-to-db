package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Sultan-Ubiquitous/sheets-to-db/database"
	"github.com/google/uuid"
)

// Product Struct (matches DB)
type Product struct {
	UUID        string  `json:"uuid"`
	ProductName string  `json:"product_name"`
	Quantity    int     `json:"quantity"`
	Price       float64 `json:"price"`
	Discount    bool    `json:"discount"`
}

// 1. GET /api/products
func GetProductsHandler(w http.ResponseWriter, r *http.Request) {
	products, err := database.GetAllProducts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// If nil (empty DB), return empty array [] instead of null
	if products == nil {
		products = []map[string]interface{}{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(products)
}

// 2. POST /api/products
func CreateProductHandler(w http.ResponseWriter, r *http.Request) {
	var p Product
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	// Generate UUID if not present
	newUUID := "u-" + uuid.New().String()[:8]

	// FIX: Explicitly set last_updated_by to 'system'
	query := "INSERT INTO product (uuid, product_name, quantity, price, discount, last_updated_by) VALUES (?, ?, ?, ?, ?, ?)"
	_, err := database.DB.Exec(query, newUUID, p.ProductName, p.Quantity, p.Price, p.Discount, "system")

	if err != nil {
		http.Error(w, "Failed to insert product: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "Product created with UUID: %s", newUUID)
}

// 3. PUT /api/products/{uuid}
// Handles partial updates (PATCH style) for inline editing
func UpdateProductHandler(w http.ResponseWriter, r *http.Request) {
	// Extract UUID from URL path manually since we aren't using a router like Chi/Mux
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, "Missing UUID", http.StatusBadRequest)
		return
	}
	id := parts[3] // /api/products/{uuid}

	// Decode partial map to support single field updates
	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Dynamically build query
	query := "UPDATE product SET "
	args := []interface{}{}

	hasUpdates := false
	for key, val := range updates {
		// whitelist allowed columns to prevent SQL injection via keys
		if key == "product_name" || key == "quantity" || key == "price" || key == "discount" {
			query += fmt.Sprintf("%s = ?, ", key)
			args = append(args, val)
			hasUpdates = true
		}
	}

	if !hasUpdates {
		http.Error(w, "No valid fields to update", http.StatusBadRequest)
		return
	}

	// --- FIX START ---
	// Force the update to be attributed to "system"
	// We append this to the query so the DB knows who did it.
	query += "last_updated_by = ?, "
	args = append(args, "system")
	// --- FIX END ---

	// Remove trailing comma
	query = strings.TrimSuffix(query, ", ")
	query += " WHERE uuid = ?"
	args = append(args, id)

	_, err := database.DB.Exec(query, args...)
	if err != nil {
		http.Error(w, "Update failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Updated"))
}

// 4. DELETE /api/products/{uuid}
func DeleteProductHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, "Missing UUID", http.StatusBadRequest)
		return
	}
	id := parts[3]

	_, err := database.DB.Exec("DELETE FROM product WHERE uuid = ?", id)
	if err != nil {
		http.Error(w, "Delete failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Deleted"))
}
