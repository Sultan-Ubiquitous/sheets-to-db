package gsheets

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Sultan-Ubiquitous/sheets-to-db/internal/config"
	"github.com/Sultan-Ubiquitous/sheets-to-db/internal/database"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

type SheetManager struct {
	Service       *sheets.Service
	SpreadsheetID string
}

// Helper to get a pointer to a string
func strPtr(s string) *string {
	return &s
}

func (s *SheetManager) InitializeSheet() error {
	// 1. Check if the sheet is already initialized
	readRange := "Sheet1!A1"
	resp, err := s.Service.Spreadsheets.Values.Get(s.SpreadsheetID, readRange).Do()
	if err != nil {
		return fmt.Errorf("failed to check sheet status: %v", err)
	}

	if len(resp.Values) > 0 && len(resp.Values[0]) > 0 {
		return nil
	}

	log.Println("Sheet appears empty. Initializing headers and formatting...")

	var requests []*sheets.Request

	// --- A. Set Header Values ---
	requests = append(requests, &sheets.Request{
		UpdateCells: &sheets.UpdateCellsRequest{
			Start: &sheets.GridCoordinate{SheetId: 0, RowIndex: 0, ColumnIndex: 0},
			Rows: []*sheets.RowData{
				{
					Values: []*sheets.CellData{
						// USE strPtr() HERE
						{UserEnteredValue: &sheets.ExtendedValue{StringValue: strPtr("UUID")}},
						{UserEnteredValue: &sheets.ExtendedValue{StringValue: strPtr("Product Name")}},
						{UserEnteredValue: &sheets.ExtendedValue{StringValue: strPtr("Quantity")}},
						{UserEnteredValue: &sheets.ExtendedValue{StringValue: strPtr("Price")}},
						{UserEnteredValue: &sheets.ExtendedValue{StringValue: strPtr("Discount")}},
						{UserEnteredValue: &sheets.ExtendedValue{StringValue: strPtr("Last Updated")}},
						{UserEnteredValue: &sheets.ExtendedValue{StringValue: strPtr("Updated By")}},
					},
				},
			},
			Fields: "userEnteredValue",
		},
	})

	// --- B. Format Header Row ---
	requests = append(requests, &sheets.Request{
		RepeatCell: &sheets.RepeatCellRequest{
			Range: &sheets.GridRange{
				SheetId:       0,
				StartRowIndex: 0, EndRowIndex: 1,
			},
			Cell: &sheets.CellData{
				UserEnteredFormat: &sheets.CellFormat{
					BackgroundColor:     &sheets.Color{Red: 0.9, Green: 0.9, Blue: 0.9},
					TextFormat:          &sheets.TextFormat{Bold: true},
					HorizontalAlignment: "CENTER",
				},
			},
			Fields: "userEnteredFormat(backgroundColor,textFormat,horizontalAlignment)",
		},
	})

	// Freeze top row
	requests = append(requests, &sheets.Request{
		UpdateSheetProperties: &sheets.UpdateSheetPropertiesRequest{
			Properties: &sheets.SheetProperties{
				SheetId:        0,
				GridProperties: &sheets.GridProperties{FrozenRowCount: 1},
			},
			Fields: "gridProperties.frozenRowCount",
		},
	})

	// --- C. Format Price Column ---
	requests = append(requests, &sheets.Request{
		RepeatCell: &sheets.RepeatCellRequest{
			Range: &sheets.GridRange{
				SheetId:          0,
				StartColumnIndex: 3, EndColumnIndex: 4,
				StartRowIndex: 1,
			},
			Cell: &sheets.CellData{
				UserEnteredFormat: &sheets.CellFormat{
					NumberFormat: &sheets.NumberFormat{Type: "CURRENCY", Pattern: "$#,##0.00"},
				},
			},
			Fields: "userEnteredFormat.numberFormat",
		},
	})

	// --- D. Format Quantity Column ---
	requests = append(requests, &sheets.Request{
		RepeatCell: &sheets.RepeatCellRequest{
			Range: &sheets.GridRange{
				SheetId:          0,
				StartColumnIndex: 2, EndColumnIndex: 3,
				StartRowIndex: 1,
			},
			Cell: &sheets.CellData{
				UserEnteredFormat: &sheets.CellFormat{HorizontalAlignment: "CENTER"},
			},
			Fields: "userEnteredFormat.horizontalAlignment",
		},
	})

	batchReq := &sheets.BatchUpdateSpreadsheetRequest{Requests: requests}
	_, err = s.Service.Spreadsheets.BatchUpdate(s.SpreadsheetID, batchReq).Do()
	return err
}

func NewSheetManager(spreadsheetID string) (*SheetManager, error) {
	ctx := context.Background()

	// 1. Get the token from our DB
	token, err := database.GetLatestToken()
	if err != nil {
		return nil, fmt.Errorf("no auth token found in DB, please login first: %v", err)
	}

	// 2. Create the OAuth Config (needed for auto-refresh)
	// We reconstruct the config using the env variables
	conf := config.GoogleOAuthConfig // Ensure your config package exposes the oauth2.Config

	// 3. Create a TokenSource
	// This is the magic. It wraps the token. If it expires,
	// the library uses the RefreshToken to get a new one automatically.
	tokenSource := conf.TokenSource(ctx, token)

	// 4. Initialize the Sheets Service
	srv, err := sheets.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, err
	}

	sm := &SheetManager{
		Service:       srv,
		SpreadsheetID: spreadsheetID,
	}

	// --- ADD THIS BLOCK ---
	// This creates the headers if they are missing
	if err := sm.InitializeSheet(); err != nil {
		log.Printf("Warning: Failed to initialize sheet headers: %v", err)
	}
	// ---------------------

	return sm, nil
}

// --- NEW HELPER: Find row index by UUID ---
func (s *SheetManager) findRowIndex(uuid string) (int, error) {
	readRange := "Sheet1!A:A"
	resp, err := s.Service.Spreadsheets.Values.Get(s.SpreadsheetID, readRange).Do()
	if err != nil {
		return -1, fmt.Errorf("failed to read sheet for lookup: %v", err)
	}

	for i, row := range resp.Values {
		if len(row) > 0 && row[0] == uuid {
			return i, nil // Returns 0-based index
		}
	}
	return -1, nil
}

func (s *SheetManager) SyncToSheet(uuid string, data map[string]interface{}) error {
	// 1. Find the Row Number
	index, err := s.findRowIndex(uuid)
	if err != nil {
		return err
	}

	// If row doesn't exist, INSERT (Append)
	if index == -1 {
		return s.appendRow(uuid, data)
	}

	// 2. Prepare Data
	rowNum := index + 1
	writeRange := fmt.Sprintf("Sheet1!B%d:G%d", rowNum, rowNum)
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	// Safely get last_updated_by, default to system if missing
	updatedBy, ok := data["last_updated_by"].(string)
	if !ok || updatedBy == "" {
		updatedBy = "system"
	}

	values := []interface{}{
		data["product_name"],
		data["quantity"],
		data["price"],
		data["discount"],
		timestamp,
		updatedBy,
	}

	valRange := &sheets.ValueRange{
		Values: [][]interface{}{values},
	}

	_, err = s.Service.Spreadsheets.Values.Update(s.SpreadsheetID, writeRange, valRange).ValueInputOption("RAW").Do()

	// REMOVED: The secondary "loopGuard" update.
	// We now write the actual user to Column G in the block above.

	if err == nil {
		log.Printf("Synced row %d in Sheets for UUID %s (Updated By: %s)", rowNum, uuid, updatedBy)
	}
	return err
}

// --- NEW METHOD: DeleteRow ---
func (s *SheetManager) DeleteRow(uuid string) error {
	index, err := s.findRowIndex(uuid)
	if err != nil {
		return err
	}

	if index == -1 {
		log.Printf("UUID %s not found in sheets, skipping delete.", uuid)
		return nil
	}

	// Prepare delete request (DeleteDimension)
	req := &sheets.Request{
		DeleteDimension: &sheets.DeleteDimensionRequest{
			Range: &sheets.DimensionRange{
				SheetId:    0,
				Dimension:  "ROWS",
				StartIndex: int64(index),
				EndIndex:   int64(index + 1),
			},
		},
	}

	batchReq := &sheets.BatchUpdateSpreadsheetRequest{
		Requests: []*sheets.Request{req},
	}

	_, err = s.Service.Spreadsheets.BatchUpdate(s.SpreadsheetID, batchReq).Do()
	if err == nil {
		log.Printf("Deleted row %d for UUID %s", index+1, uuid)
	}
	return err
}

func (s *SheetManager) appendRow(uuid string, data map[string]interface{}) error {
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	// Safely get last_updated_by
	updatedBy, ok := data["last_updated_by"].(string)
	if !ok || updatedBy == "" {
		updatedBy = "system"
	}

	values := []interface{}{
		uuid,
		data["product_name"],
		data["quantity"],
		data["price"],
		data["discount"],
		timestamp,
		updatedBy, // CHANGE: Use actual DB value
	}

	valRange := &sheets.ValueRange{
		Values: [][]interface{}{values},
	}

	_, err := s.Service.Spreadsheets.Values.Append(s.SpreadsheetID, "Sheet1!A1", valRange).ValueInputOption("RAW").Do()
	return err
}

func (s *SheetManager) ClearAndOverwrite(products []map[string]interface{}) error {
	// 1. Clear existing data (keeping headers)
	// Assuming headers are in Row 1, we clear everything from Row 2 downwards
	clearRange := "Sheet1!A2:Z"
	_, err := s.Service.Spreadsheets.Values.Clear(s.SpreadsheetID, clearRange, &sheets.ClearValuesRequest{}).Do()
	if err != nil {
		return fmt.Errorf("failed to clear sheet: %v", err)
	}

	// 2. Prepare Data for Bulk Write
	var valueRange sheets.ValueRange
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	for _, p := range products {
		// --- CHANGE START ---
		// Determine the "Updated By" value
		updatedBy := "initial_sync" // Default fallback
		if val, ok := p["last_updated_by"].(string); ok && val != "" {
			updatedBy = val
		}
		// --- CHANGE END ---

		row := []interface{}{
			p["uuid"],
			p["product_name"],
			p["quantity"],
			p["price"],
			p["discount"],
			timestamp,
			updatedBy, // Use the determined value here
		}
		valueRange.Values = append(valueRange.Values, row)
	}

	if len(valueRange.Values) == 0 {
		return nil // Nothing to write
	}

	// 3. Write new data starting at A2
	writeRange := "Sheet1!A2"
	_, err = s.Service.Spreadsheets.Values.Update(s.SpreadsheetID, writeRange, &valueRange).ValueInputOption("RAW").Do()

	log.Printf("Successfully performed Initial Sync of %d products", len(products))
	return err
}
