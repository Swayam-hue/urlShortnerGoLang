package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

type ShortenRequest struct {
	URL string `json:"url"`
}

type ShortenResponse struct {
	ShortCode string `json:"shortcode"`
	ShortURL  string `json:"short_url"`
}

const BASE62 = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func encodeBase62(num int64) string {

	if num == 0 {
		return "0"
	}

	var result []byte

	for num > 0 {

		rem := num % 62

		result = append(
			[]byte{BASE62[rem]},
			result...,
		)

		num /= 62
	}

	return string(result)
}

func shortenURL(
	pool *pgxpool.Pool,
	rdb *redis.Client,
) http.HandlerFunc {

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

		ctx := context.Background()


		var existingCode string

		err = pool.QueryRow(
			ctx,
			`
			SELECT shortcode
			FROM urltable_2
			WHERE longurl = $1
			`,
			req.URL,
		).Scan(&existingCode)

		if err == nil {

			resp := ShortenResponse{
				ShortCode: existingCode,
				ShortURL: os.Getenv("BASE_URL") + "/" + existingCode,
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}


		counter, err := rdb.Incr(
			ctx,
			"url_counter",
		).Result()

		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		shortCode := encodeBase62(counter)


		_, err = pool.Exec(
			ctx,
			`
			INSERT INTO urltable_2(
				shortcode,
				longurl
			)
			VALUES($1,$2)
			`,
			shortCode,
			req.URL,
		)

		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}


		rdb.Set(
			ctx,
			"url:"+shortCode,
			req.URL,
			24*time.Hour,
		)

		resp := ShortenResponse{
			ShortCode: shortCode,
			ShortURL: os.Getenv("BASE_URL") + "/" + shortCode,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func redirectHandler(
	pool *pgxpool.Pool,
	rdb *redis.Client,
) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		code := strings.TrimPrefix(
			r.URL.Path,
			"/",
		)

		if code == "" {
			http.Error(w, "Missing shortcode", 400)
			return
		}

		ctx := context.Background()


		cachedURL, err := rdb.Get(
			ctx,
			"url:"+code,
		).Result()

		if err == nil {

			fmt.Println("CACHE HIT")

			http.Redirect(
				w,
				r,
				cachedURL,
				http.StatusFound,
			)

			return
		}

		fmt.Println("CACHE MISS")


		var longURL string

		err = pool.QueryRow(
			ctx,
			`
			SELECT longurl
			FROM urltable_2
			WHERE shortcode = $1
			`,
			code,
		).Scan(&longURL)

		if err != nil {
			http.NotFound(w, r)
			return
		}

		

		rdb.Set(
			ctx,
			"url:"+code,
			longURL,
			24*time.Hour,
		)

		http.Redirect(
			w,
			r,
			longURL,
			http.StatusFound,
		)
	}
}

func main() {

	godotenv.Load()

	ctx := context.Background()

	dbURL := os.Getenv("DATABASE_URL")

	pool, err := pgxpool.New(
		ctx,
		dbURL,
	)

	if err != nil {
		log.Fatal(err)
	}

	defer pool.Close()

	redisURL := os.Getenv("REDIS_URL")

	opt, err := redis.ParseURL(redisURL)

	if err != nil {
		log.Fatal(err)
	}

	rdb := redis.NewClient(opt)

	_, err = rdb.Ping(ctx).Result()

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Redis Connected")


	http.HandleFunc(
		"/shorten",
		shortenURL(pool, rdb),
	)

	http.HandleFunc(
		"/",
		redirectHandler(pool, rdb),
	)

	fmt.Println("Server running on :8080")

	log.Fatal(
		http.ListenAndServe(
			":8080",
			nil,
		),
	)
}