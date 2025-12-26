package gsheets

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/Sultan-Ubiquitous/sheets-to-db/internal/config"
	"github.com/Sultan-Ubiquitous/sheets-to-db/internal/database"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

// CreateAndSeedSheetHandler handles the creation and seeding of a Google Sheet
func CreateAndSeedSheetHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Retrieve the latest token from the database
	token, err := database.GetLatestToken()
	if err != nil {
		http.Error(w, "User not authenticated: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// 2. Create a Sheets Service client
	ctx := context.Background()
	// We need the OAuth config to recreate a valid client (handles refreshing)
	tokenSource := config.GoogleOAuthConfig.TokenSource(ctx, token)

	srv, err := sheets.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		http.Error(w, "Unable to retrieve Sheets client: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 3. Create a new Spreadsheet
	spreadsheet := &sheets.Spreadsheet{
		Properties: &sheets.SpreadsheetProperties{
			Title: "Product Inventory (Seeded)",
		},
	}

	resp, err := srv.Spreadsheets.Create(spreadsheet).Do()
	if err != nil {
		http.Error(w, "Unable to create spreadsheet: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("Created spreadsheet ID: %s", resp.SpreadsheetId)

	// 4. Store the ID in MySQL
	// We use a constant name "inventory" so we can find it later
	if err := database.SaveSheetID("inventory", resp.SpreadsheetId); err != nil {
		http.Error(w, "Failed to save sheet ID to DB: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 5. Seed Data (Headers + Dummy Data matching your 'product' schema)
	var vr sheets.ValueRange

	// Data structure: Rows of columns
	vr.Values = append(vr.Values, []interface{}{"UUID", "Product Name", "Quantity", "Price", "Discount"}) // Header
	vr.Values = append(vr.Values, []interface{}{"u-101", "Gaming Mouse", 50, 49.99, false})
	vr.Values = append(vr.Values, []interface{}{"u-102", "Mechanical Keyboard", 30, 120.00, true})
	vr.Values = append(vr.Values, []interface{}{"u-103", "USB-C Cable", 100, 9.99, false})

	// Write to the sheet (A1 notation)
	writeRange := "Sheet1!A1"
	_, err = srv.Spreadsheets.Values.Update(resp.SpreadsheetId, writeRange, &vr).ValueInputOption("RAW").Do()
	if err != nil {
		http.Error(w, "Unable to write data to sheet: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Response
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"success", "message":"Sheet created and seeded", "spreadsheet_id": "%s", "link": "%s"}`, resp.SpreadsheetId, resp.SpreadsheetUrl)
}
