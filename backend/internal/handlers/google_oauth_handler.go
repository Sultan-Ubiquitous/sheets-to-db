package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Sultan-Ubiquitous/sheets-to-db/internal/config"
	"github.com/Sultan-Ubiquitous/sheets-to-db/internal/database"
	"golang.org/x/oauth2"
)

func GoogleLoginHandler(w http.ResponseWriter, r *http.Request) {
	url := config.GoogleOAuthConfig.AuthCodeURL(
		"state-token",
		oauth2.AccessTypeOffline,
		oauth2.ApprovalForce,
	)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

type GoogleUser struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

func GoogleCallbackHandler(w http.ResponseWriter, r *http.Request, loginSignal chan<- struct{}) {
	code := r.URL.Query().Get("code")

	token, err := config.GoogleOAuthConfig.Exchange(
		context.Background(),
		code,
	)

	if err != nil {
		http.Error(w, "OAuth exchange failed", http.StatusInternalServerError)
		return
	}

	client := config.GoogleOAuthConfig.Client(context.Background(), token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		http.Error(w, "Failed to fetch user info", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var userInfo GoogleUser
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		http.Error(w, "Failed to decode user info", http.StatusInternalServerError)
		return
	}

	err = database.UpsertToken(userInfo.Email, token)
	if err != nil {
		fmt.Printf("DB Error: %v\n", err)
		http.Error(w, "Failed to save token", http.StatusInternalServerError)
		return
	}

	select {
	case loginSignal <- struct{}{}:
		fmt.Println("Signaled worker that login is complete.")
	default:
		// Channel already has a signal or no one is waiting; harmless.
	}

	fmt.Fprintf(w, "Login Successful! Token stored for %s", userInfo.Email)
}
