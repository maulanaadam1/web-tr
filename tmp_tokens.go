package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, err := sql.Open("sqlite3", "streams.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT email, public_token FROM users WHERE public_token IS NOT NULL AND public_token != '' LIMIT 5")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var email, token string
		if err := rows.Scan(&email, &token); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Email: %s | Token: %s\n", email, token)
	}
}
