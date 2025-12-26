package main

import (
	"log"
	"net/http"
	"os"

	"github.com/Sultan-Ubiquitous/sheets-to-db/cdc"
	"github.com/Sultan-Ubiquitous/sheets-to-db/config"
	"github.com/Sultan-Ubiquitous/sheets-to-db/database"
	"github.com/Sultan-Ubiquitous/sheets-to-db/gsheets"
	"github.com/Sultan-Ubiquitous/sheets-to-db/handlers"
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

	binlogFile, binlogPos, err := database.GetMasterStatus()
	if err != nil {
		log.Fatalf("Error getting master status: %v", err)
	}
	log.Printf("Snapshot taken. Resume CDC from %s:%d", binlogFile, binlogPos)

	authReadySignal := make(chan struct{}, 1)
	syncChannel := make(chan cdc.SyncEvent, 100)

	go cdc.StartListener(syncChannel, binlogFile, binlogPos)

	spreadsheetID := os.Getenv("SPREADSHEET_ID")

	log.Printf("DEBUG: Loaded SPREADSHEET_ID from env: '%s'", spreadsheetID)
	if spreadsheetID == "" {
		log.Fatal("CRITICAL ERROR: SPREADSHEET_ID is empty! Check your .env file.")
	}

	go func() {
		log.Println("Starting Sheet Sync Worker...")

		sm, err := gsheets.NewSheetManager(spreadsheetID)

		if err != nil {
			log.Println("Worker STALLED. Waiting for login...")
			<-authReadySignal
			log.Println("Resuming...")
			sm, _ = gsheets.NewSheetManager(spreadsheetID)
		}

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
			case <-authReadySignal:
				log.Println("Hot Reload: Refreshing Sheet Manager with new token...")
				newSm, err := gsheets.NewSheetManager(spreadsheetID)
				if err == nil {
					sm = newSm
					log.Println("Sheet Manager refreshed successfully!")

					products, _ := database.GetAllProducts()
					sm.ClearAndOverwrite(products)
				} else {
					log.Printf("Failed to refresh manager: %v", err)
				}

			case event := <-syncChannel:
				if sm == nil {
					log.Println("Skipping event: Sheet Manager not ready")
					continue
				}

				log.Printf("Processing Event: %s %s", event.Action, event.RowID)

				var err error

				if event.Action == "delete" {
					err = sm.DeleteRow(event.RowID)
				} else {
					err = sm.SyncToSheet(event.RowID, event.Data)
				}

				if err != nil {
					log.Printf("Error syncing (%s): %v", event.Action, err)
				}
			}
		}
	}()

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	http.HandleFunc("/auth/google/login", handlers.GoogleLoginHandler)
	http.HandleFunc("/auth/google/callback", func(w http.ResponseWriter, r *http.Request) {
		handlers.GoogleCallbackHandler(w, r, authReadySignal)
	})

	http.HandleFunc("/api/products", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handlers.GetProductsHandler(w, r)
		} else if r.Method == http.MethodPost {
			handlers.CreateProductHandler(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/api/products/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			handlers.UpdateProductHandler(w, r)
		} else if r.Method == http.MethodDelete {
			handlers.DeleteProductHandler(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/api/webhook/sheets", handlers.SheetWebhookHandler)

	port := ":8080"
	log.Printf("Server starting on http://localhost%s", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
