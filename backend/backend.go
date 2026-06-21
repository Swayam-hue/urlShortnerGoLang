package main
import (
	"crypto/sha256"
	"context"
	"fmt"
	"os"
	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
	"github.com/jxskiss/base62"
	"log"
	"net/http"
	"encoding/json"
	"strings"
)

type ShortenRequest struct {
	URL string `json:"url"`
}

type ShortenResponse struct {
	ShortCode string `json:"shortcode"`
	ShortURL  string `json:"short_url"`
}

func shortenURL(conn *pgx.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req ShortenRequest

		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		h := sha256.New()
		h.Write([]byte(req.URL))

		hash := h.Sum(nil)
		shortCode := base62.EncodeToString(hash)[:8]

		_, err = conn.Exec(
			context.Background(),
			`INSERT INTO urltable_2(shortcode,longurl)
			 VALUES($1,$2)
			 ON CONFLICT(shortcode) DO NOTHING`,
			shortCode,
			req.URL,
		)

		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		resp := ShortenResponse{
			ShortCode: shortCode,
			ShortURL:  "http://localhost:8080/" + shortCode,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func redirectHandler(conn *pgx.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		code := strings.TrimPrefix(r.URL.Path, "/")

		if code == "" {
			http.Error(w, "Missing shortcode", 400)
			return
		}

		var longURL string

		err := conn.QueryRow(
			context.Background(),
			"SELECT longurl FROM urltable_2 WHERE shortcode=$1",
			code,
		).Scan(&longURL)

		if err != nil {
			http.NotFound(w, r)
			return
		}

		http.Redirect(w, r, longURL, http.StatusFound)
	}
}



func main() {

	// load .env
	godotenv.Load()

	connString := os.Getenv("DATABASE_URL")

	ctx := context.Background()

	conn, err := pgx.Connect(ctx, connString)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close(ctx)

	http.HandleFunc("/shorten", shortenURL(conn))
	http.HandleFunc("/", redirectHandler(conn))

	fmt.Println("Server running on :8080")

	log.Fatal(http.ListenAndServe(":8080", nil))
}