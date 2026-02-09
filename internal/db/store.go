package db

import (
	"database/sql"
	"fmt"
	"log"
	"web-tr/internal/models"

	_ "github.com/lib/pq"
)

type Store struct {
	db *sql.DB
}

func NewStore(connStr string) (*Store, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping db: %w", err)
	}

	s := &Store{db: db}
	if err := s.Init(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Store) Init() error {
	query := `
	CREATE TABLE IF NOT EXISTS streams (
		id SERIAL PRIMARY KEY,
		name TEXT UNIQUE NOT NULL,
		url TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`
	_, err := s.db.Exec(query)
	return err
}

func (s *Store) GetStreams() ([]models.Stream, error) {
	rows, err := s.db.Query("SELECT name, url FROM streams ORDER BY name ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var st models.Stream
		if err := rows.Scan(&st.Name, &st.URL); err != nil {
			log.Printf("Error scanning row: %v", err)
			continue
		}
		streams = append(streams, st)
	}
	return streams, nil
}

func (s *Store) AddStream(st models.Stream) error {
	_, err := s.db.Exec("INSERT INTO streams (name, url) VALUES ($1, $2) ON CONFLICT (name) DO UPDATE SET url = $2", st.Name, st.URL)
	return err
}

func (s *Store) RemoveStream(name string) error {
	_, err := s.db.Exec("DELETE FROM streams WHERE name = $1", name)
	return err
}

func (s *Store) UpdateStream(oldName, newName, url string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if oldName != newName {
		// Check if new name exists
		var exists bool
		err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM streams WHERE name = $1)", newName).Scan(&exists)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("stream name '%s' already exists", newName)
		}

		// Update name and url
		_, err = tx.Exec("UPDATE streams SET name = $1, url = $2 WHERE name = $3", newName, url, oldName)
		if err != nil {
			return err
		}
	} else {
		// Just update url
		_, err = tx.Exec("UPDATE streams SET url = $1 WHERE name = $2", url, newName)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}
