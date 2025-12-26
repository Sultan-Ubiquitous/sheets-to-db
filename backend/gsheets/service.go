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

func (s *SheetManager) SyncToSheet(uuid string, data map[string]interface{}) error {
	// 1. Find the Row Number (Scan Column A)
	// In production, you would cache this map (UUID -> Row#) in Redis to avoid reading the whole sheet every time.
	readRange := "Sheet1!A:A"
	resp, err := s.Service.Spreadsheets.Values.Get(s.SpreadsheetID, readRange).Do()
	if err != nil {
		return fmt.Errorf("failed to read sheet for lookup: %v", err)
	}

	rowIndex := -1
	for i, row := range resp.Values {
		if len(row) > 0 && row[0] == uuid {
			rowIndex = i + 1 // Sheets are 1-indexed
			break
		}
	}

	// If row doesn't exist, we should INSERT (Append)
	if rowIndex == -1 {
		return s.appendRow(uuid, data)
	}

	// 2. Update the existing row
	// Mapping: A=UUID, B=Name, C=Qty, D=Price, E=Discount, F=Updated, G=LastBy
	writeRange := fmt.Sprintf("Sheet1!B%d:E%d", rowIndex, rowIndex)
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	values := []interface{}{
		data["product_name"],
		data["quantity"],
		data["price"],
		data["discount"],
		timestamp,
		"sync_bot",
	}

	valRange := &sheets.ValueRange{
		Values: [][]interface{}{values},
	}

	_, err = s.Service.Spreadsheets.Values.Update(s.SpreadsheetID, writeRange, valRange).ValueInputOption("RAW").Do()
	if err != nil {
		return err
	}

	// Update the "Last Updated By" column (Col G) to prevent loops
	// We do this separately or include it above. Let's force it here.
	loopGuardRange := fmt.Sprintf("Sheet1!G%d", rowIndex)
	loopGuardVal := &sheets.ValueRange{Values: [][]interface{}{{"sync_bot"}}}
	s.Service.Spreadsheets.Values.Update(s.SpreadsheetID, loopGuardRange, loopGuardVal).ValueInputOption("RAW").Do()

	log.Printf("Synced row %d in Sheets for UUID %s", rowIndex, uuid)
	return nil
}

func (s *SheetManager) appendRow(uuid string, data map[string]interface{}) error {
	// Construct the full row
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	values := []interface{}{
		uuid,
		timestamp,
		data["product_name"],
		data["quantity"],
		data["price"],
		data["discount"],
		timestamp,  // timestamp (let sheets handle it or pass it)
		"sync_bot", // Loop guard
	}

	valRange := &sheets.ValueRange{
		Values: [][]interface{}{values},
	}

	_, err := s.Service.Spreadsheets.Values.Append(s.SpreadsheetID, "Sheet1!A1", valRange).ValueInputOption("RAW").Do()
	return err
}
