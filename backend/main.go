package main

import (
	"log"
	"net/http"

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

	http.HandleFunc("/auth/google/login", handlers.GoogleLoginHandler)
	http.HandleFunc("/auth/google/callback", handlers.GoogleCallbackHandler)

	port := ":8080"
	log.Printf("Server starting on http://localhost%s", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
