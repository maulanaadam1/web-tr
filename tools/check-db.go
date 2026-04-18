package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"web-tr/internal/config"

	_ "modernc.org/sqlite"
)

func main() {
	// 1. Try SQLite
	fmt.Println("=== Checking SQLite (streams.db) ===")
	if _, err := os.Stat("streams.db"); err == nil {
		db, err := sql.Open("sqlite", "streams.db")
		if err != nil {
			log.Printf("Error opening DB: %v\n", err)
		} else {
			defer db.Close()
			rows, err := db.Query("SELECT name, url, backend FROM streams ORDER BY name")
			if err != nil {
				// Table might not exist
				fmt.Printf("Error querying DB (Table might be missing): %v\n", err)
			} else {
				count := 0
				for rows.Next() {
					var name, url, backend string
					if err := rows.Scan(&name, &url, &backend); err != nil {
						log.Printf("Scan error: %v\n", err)
						continue
					}
					count++
					fmt.Printf("%d. Name: %s\n   URL: %s\n   Backend: %s\n\n", count, name, url, backend)
				}
				rows.Close()
				if count > 0 {
					fmt.Printf("Total found in DB: %d streams\n", count)
					return // Found streams in DB, exit
				}
				fmt.Println("Database exists but is empty.")
			}
		}
	} else {
		fmt.Println("streams.db not found.")
	}

	// 2. Try Config File
	fmt.Println("\n=== Checking Config (go2rtc.yaml) ===")
	cfgMgr := config.NewConfigManager(filepath.Join("data", "go2rtc.yaml"))
	streams, err := cfgMgr.GetStreams()
	if err != nil {
		log.Fatalf("Error reading config: %v", err)
	}

	if len(streams) == 0 {
		fmt.Println("No streams found in go2rtc.yaml")
	} else {
		for i, s := range streams {
			fmt.Printf("%d. Name: %s\n   URL: %s\n   Backend: Go2RTC (File Mode)\n\n", i+1, s.Name, s.URL)
		}
		fmt.Printf("Total found in Config: %d streams\n", len(streams))
	}
}
