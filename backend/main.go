package main

import (
	"log"
	"net/http"
	"os"

	"github.com/Sultan-Ubiquitous/sheets-to-db/cdc"
	"github.com/Sultan-Ubiquitous/sheets-to-db/gsheets"
	"github.com/Sultan-Ubiquitous/sheets-to-db/internal/config"
	"github.com/Sultan-Ubiquitous/sheets-to-db/internal/database"
	"github.com/Sultan-Ubiquitous/sheets-to-db/internal/handlers"
	"github.com/joho/godotenv"
)

func main() {

	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, relying on system environment variables")
	}

	config.LoadConfig()

	dsn := "replicator:password@tcp(127.0.0.1:3306)/interndb?parseTime=true"

	log.Println("Connecting to Database")

	if err := database.InitDB(dsn); err != nil {
		log.Fatalf("Fatal error: Could not connect to database: %v", err)
	}

	log.Println("Successfully connected to MySQL database!")

	// --- CHANGE 1: Capture Current State ---
	// Get the current binlog position BEFORE we start doing anything else.
	// This marks the "Now" point.
	binlogFile, binlogPos, err := database.GetMasterStatus()
	if err != nil {
		log.Fatalf("Error getting master status: %v", err)
	}
	log.Printf("Snapshot taken. Resume CDC from %s:%d", binlogFile, binlogPos)

	authReadySignal := make(chan struct{}, 1)
	syncChannel := make(chan cdc.SyncEvent, 100)

	// --- CHANGE 2: Start Listener from Captured Position ---
	// We pass the file/pos so it ignores old history and only listens to NEW events.
	go cdc.StartListener(syncChannel, binlogFile, binlogPos)

	spreadsheetID := os.Getenv("SPREADSHEET_ID")

	log.Printf("DEBUG: Loaded SPREADSHEET_ID from env: '%s'", spreadsheetID)
	if spreadsheetID == "" {
		log.Fatal("CRITICAL ERROR: SPREADSHEET_ID is empty! Check your .env file.")
	}

	go func() {
		log.Println("Starting Sheet Sync Worker...")

		// Initialize Manager
		sm, err := gsheets.NewSheetManager(spreadsheetID)

		// Initial Block: If completely missing token, wait here first.
		if err != nil {
			log.Println("Worker STALLED. Waiting for login...")
			<-authReadySignal
			log.Println("Resuming...")
			sm, _ = gsheets.NewSheetManager(spreadsheetID) // Retry
		}

		// --- CHANGE 3: Perform Initial Sync (Snapshot) ---
		// Now that we have a manager, wipe the sheet and dump the current DB state
		// so it matches the "Snapshot" we took at the start.
		if sm != nil {
			log.Println("Performing Initial Full Sync...")
			products, err := database.GetAllProducts()
			if err != nil {
				log.Printf("Error fetching initial products: %v", err)
			} else {
				if err := sm.ClearAndOverwrite(products); err != nil {
					log.Printf("Error performing initial sheet overwrite: %v", err)
				}
			}
		}

		log.Println("Sheet Manager Running via Event Loop")

		for {
			select {
			// Case 1: We receive a new Login Signal (User re-authenticated)
			case <-authReadySignal:
				log.Println("Hot Reload: Refreshing Sheet Manager with new token...")
				newSm, err := gsheets.NewSheetManager(spreadsheetID)
				if err == nil {
					sm = newSm
					log.Println("Sheet Manager refreshed successfully!")

					// Optional: Trigger another full sync here if you want to ensure
					// the sheet is fresh after a re-login.
					products, _ := database.GetAllProducts()
					sm.ClearAndOverwrite(products)
				} else {
					log.Printf("Failed to refresh manager: %v", err)
				}

			// Case 2: We receive a Database Event
			case event := <-syncChannel:
				if sm == nil {
					log.Println("Skipping event: Sheet Manager not ready")
					continue
				}

				log.Printf("Processing Event: %s %s", event.Action, event.RowID)
				err := sm.SyncToSheet(event.RowID, event.Data)
				if err != nil {
					log.Printf("Error syncing: %v", err)
				}
			}
		}
	}()

	http.HandleFunc("/auth/google/login", handlers.GoogleLoginHandler)
	http.HandleFunc("/auth/google/callback", func(w http.ResponseWriter, r *http.Request) {
		handlers.GoogleCallbackHandler(w, r, authReadySignal)
	})
	http.HandleFunc("/api/create-sheet", gsheets.CreateAndSeedSheetHandler)

	port := ":8080"
	log.Printf("Server starting on http://localhost%s", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
