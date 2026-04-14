package db

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"web-tr/internal/models"

	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

type Store struct {
	db     *sql.DB
	dbType string // "postgres" or "sqlite"
}

func NewStore(connStr string) (*Store, error) {
	// Auto-detect database type from connection string
	var driver string
	var dsn string

	if strings.HasPrefix(connStr, "postgres://") || strings.HasPrefix(connStr, "postgresql://") || strings.Contains(connStr, "sslmode=") {
		driver = "postgres"
		dsn = connStr
	} else {
		// Default to sqlite for local files
		driver = "sqlite"
		dsn = strings.TrimPrefix(connStr, "file:")
		dsn = strings.TrimPrefix(dsn, "sqlite:")
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping db: %w", err)
	}

	s := &Store{
		db:     db,
		dbType: driver,
	}

	if err := s.Init(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Store) Init() error {
	var query string

	// Database-specific SQL
	if s.dbType == "sqlite" {
		query = `
		CREATE TABLE IF NOT EXISTS streams (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL,
			url TEXT NOT NULL,
			backend TEXT DEFAULT 'go2rtc',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`
	} else {
		// PostgreSQL
		query = `
		CREATE TABLE IF NOT EXISTS streams (
			id SERIAL PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			url TEXT NOT NULL,
			backend TEXT DEFAULT 'go2rtc',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`
	}

	if _, err := s.db.Exec(query); err != nil {
		return err
	}

	// Create users table
	var userQuery string
	if s.dbType == "sqlite" {
		userQuery = `
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			salt TEXT NOT NULL,
			role TEXT DEFAULT 'admin',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`
	} else {
		// PostgreSQL
		userQuery = `
		CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			salt TEXT NOT NULL,
			role TEXT DEFAULT 'admin',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`
	}

	if _, err := s.db.Exec(userQuery); err != nil {
		return err
	}

	// Add backend column if it doesn't exist (migration)
	if s.dbType == "sqlite" {
		cols := []string{
			"full_name TEXT DEFAULT ''",
			"email TEXT DEFAULT ''",
			"whatsapp TEXT DEFAULT ''",
			"is_active BOOLEAN DEFAULT 1",
			"broadcast_notifications BOOLEAN DEFAULT 0",
			"notification_paid BOOLEAN DEFAULT 0",
		}
		for _, colDef := range cols {
			name := strings.Split(colDef, " ")[0]
			var count int
			_ = s.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('users') WHERE name=?", name).Scan(&count)
			if count == 0 {
				_, _ = s.db.Exec(fmt.Sprintf("ALTER TABLE users ADD COLUMN %s", colDef))
			}
		}

		// Migrate streams table columns
		streamCols := []string{
			"lat REAL DEFAULT 0",
			"lng REAL DEFAULT 0",
			"is_enabled BOOLEAN DEFAULT 1",
		}
		for _, colDef := range streamCols {
			name := strings.Split(colDef, " ")[0]
			var count int
			_ = s.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('streams') WHERE name=?", name).Scan(&count)
			if count == 0 {
				_, _ = s.db.Exec(fmt.Sprintf("ALTER TABLE streams ADD COLUMN %s", colDef))
			}
		}

	} else {
		// PostgreSQL migrations
		alterQuery := `
		ALTER TABLE users 
		ADD COLUMN IF NOT EXISTS full_name TEXT DEFAULT '',
		ADD COLUMN IF NOT EXISTS email TEXT DEFAULT '',
		ADD COLUMN IF NOT EXISTS whatsapp TEXT DEFAULT '',
		ADD COLUMN IF NOT EXISTS is_active BOOLEAN DEFAULT TRUE,
		ADD COLUMN IF NOT EXISTS broadcast_notifications BOOLEAN DEFAULT FALSE,
		ADD COLUMN IF NOT EXISTS notification_paid BOOLEAN DEFAULT FALSE;
		ALTER TABLE streams
		ADD COLUMN IF NOT EXISTS lat DOUBLE PRECISION DEFAULT 0,
		ADD COLUMN IF NOT EXISTS lng DOUBLE PRECISION DEFAULT 0,
		ADD COLUMN IF NOT EXISTS is_enabled BOOLEAN DEFAULT TRUE;`
		_, _ = s.db.Exec(alterQuery)
	}

	return nil
}

func (s *Store) GetStreams() ([]models.Stream, error) {
	rows, err := s.db.Query("SELECT name, url, COALESCE(backend, 'go2rtc') as backend, COALESCE(lat, 0) as lat, COALESCE(lng, 0) as lng, COALESCE(is_enabled, 1) as is_enabled FROM streams ORDER BY name ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var st models.Stream
		if err := rows.Scan(&st.Name, &st.URL, &st.Backend, &st.Lat, &st.Lng, &st.Enabled); err != nil {
			log.Printf("Error scanning row: %v", err)
			continue
		}
		// Default to go2rtc if empty
		if st.Backend == "" {
			st.Backend = "go2rtc"
		}
		streams = append(streams, st)
	}
	return streams, nil
}

func (s *Store) AddStream(st models.Stream) error {
	// Default to go2rtc if backend not specified
	if st.Backend == "" {
		st.Backend = "go2rtc"
	}

	var query string
	if s.dbType == "sqlite" {
		query = "INSERT INTO streams (name, url, backend, lat, lng, is_enabled) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT (name) DO UPDATE SET url = ?, backend = ?, lat = ?, lng = ?, is_enabled = ?"
		_, err := s.db.Exec(query, st.Name, st.URL, st.Backend, st.Lat, st.Lng, st.Enabled, st.URL, st.Backend, st.Lat, st.Lng, st.Enabled)
		return err
	} else {
		query = "INSERT INTO streams (name, url, backend, lat, lng, is_enabled) VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (name) DO UPDATE SET url = $2, backend = $3, lat = $4, lng = $5, is_enabled = $6"
		_, err := s.db.Exec(query, st.Name, st.URL, st.Backend, st.Lat, st.Lng, st.Enabled)
		return err
	}
}

func (s *Store) RemoveStream(name string) error {
	var query string
	if s.dbType == "sqlite" {
		query = "DELETE FROM streams WHERE name = ?"
	} else {
		query = "DELETE FROM streams WHERE name = $1"
	}
	_, err := s.db.Exec(query, name)
	return err
}

func (s *Store) ClearAllStreams() error {
	_, err := s.db.Exec("DELETE FROM streams")
	return err
}

func (s *Store) UpdateStream(oldName, newName, url, backend string, lat, lng float64, enabled bool) error {
	newName = strings.TrimSpace(newName)
	oldName = strings.TrimSpace(oldName)

	// Default to go2rtc if backend not specified
	if backend == "" {
		backend = "go2rtc"
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var checkQuery, updateNameQuery, updateUrlQuery string
	if s.dbType == "sqlite" {
		checkQuery = "SELECT EXISTS(SELECT 1 FROM streams WHERE name = ?)"
		updateNameQuery = "UPDATE streams SET name = ?, url = ?, backend = ?, lat = ?, lng = ?, is_enabled = ? WHERE name = ?"
		updateUrlQuery = "UPDATE streams SET url = ?, backend = ?, lat = ?, lng = ?, is_enabled = ? WHERE name = ?"
	} else {
		checkQuery = "SELECT EXISTS(SELECT 1 FROM streams WHERE name = $1)"
		updateNameQuery = "UPDATE streams SET name = $1, url = $2, backend = $3, lat = $4, lng = $5, is_enabled = $6 WHERE name = $7"
		updateUrlQuery = "UPDATE streams SET url = $1, backend = $2, lat = $3, lng = $4, is_enabled = $5 WHERE name = $6"
	}

	if oldName != newName {
		// Check if new name exists
		var exists bool
		err := tx.QueryRow(checkQuery, newName).Scan(&exists)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("stream name '%s' already exists", newName)
		}

		// Update name, url, and backend
		_, err = tx.Exec(updateNameQuery, newName, url, backend, lat, lng, enabled, oldName)
		if err != nil {
			return err
		}
	} else {
		// Just update url and backend
		_, err = tx.Exec(updateUrlQuery, url, backend, lat, lng, enabled, newName)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) SetStreamStatus(name string, enabled bool) error {
	var query string
	if s.dbType == "sqlite" {
		query = "UPDATE streams SET is_enabled = ? WHERE name = ?"
	} else {
		query = "UPDATE streams SET is_enabled = $1 WHERE name = $2"
	}
	_, err := s.db.Exec(query, enabled, name)
	return err
}

func (s *Store) Close() error {
	return s.db.Close()
}

// --- User Management Methods ---

// HashPassword generates a salted SHA256 hash
func HashPassword(password, salt string) string {
	hash := sha256.Sum256([]byte(password + salt))
	return hex.EncodeToString(hash[:])
}

func GenerateSalt() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Store) CreateUserFull(u models.User, password string) error {
	salt := GenerateSalt()
	hash := HashPassword(password, salt)

	var query string
	if s.dbType == "sqlite" {
		query = "INSERT INTO users (username, password_hash, salt, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	} else {
		query = "INSERT INTO users (username, password_hash, salt, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)"
	}

	_, err := s.db.Exec(query, u.Username, hash, salt, u.Role, u.FullName, u.Email, u.Whatsapp, u.IsActive, u.BroadcastNotifications, u.NotificationPaid)
	return err
}

func (s *Store) CreateUser(username, password, role string) error {
	return s.CreateUserFull(models.User{Username: username, Role: role, IsActive: true}, password)
}

func (s *Store) GetUserByUsername(username string) (*models.User, error) {
	var user models.User
	var query string
	if s.dbType == "sqlite" {
		query = "SELECT id, username, password_hash, salt, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid, created_at FROM users WHERE username = ?"
	} else {
		query = "SELECT id, username, password_hash, salt, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid, created_at FROM users WHERE username = $1"
	}

	err := s.db.QueryRow(query, username).
		Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Salt, &user.Role, &user.FullName, &user.Email, &user.Whatsapp, &user.IsActive, &user.BroadcastNotifications, &user.NotificationPaid, &user.CreatedAt)
	
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // User not found
		}
		return nil, err
	}
	return &user, nil
}

func (s *Store) GetAllUsers() ([]models.User, error) {
	rows, err := s.db.Query("SELECT id, username, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid, created_at FROM users ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.FullName, &u.Email, &u.Whatsapp, &u.IsActive, &u.BroadcastNotifications, &u.NotificationPaid, &u.CreatedAt); err != nil {
			log.Printf("Error scanning user row: %v", err)
			continue
		}
		users = append(users, u)
	}
	return users, nil
}

func (s *Store) UpdateUserPassword(id int, newPassword string) error {
	salt := GenerateSalt()
	hash := HashPassword(newPassword, salt)

	var query string
	if s.dbType == "sqlite" {
		query = "UPDATE users SET password_hash = ?, salt = ? WHERE id = ?"
	} else {
		query = "UPDATE users SET password_hash = $1, salt = $2 WHERE id = $3"
	}

	_, err := s.db.Exec(query, hash, salt, id)
	return err
}

func (s *Store) UpdateUserFull(u models.User) error {
	var query string
	if s.dbType == "sqlite" {
		query = "UPDATE users SET role = ?, full_name = ?, email = ?, whatsapp = ?, is_active = ?, broadcast_notifications = ?, notification_paid = ? WHERE id = ?"
	} else {
		query = "UPDATE users SET role = $1, full_name = $2, email = $3, whatsapp = $4, is_active = $5, broadcast_notifications = $6, notification_paid = $7 WHERE id = $8"
	}

	_, err := s.db.Exec(query, u.Role, u.FullName, u.Email, u.Whatsapp, u.IsActive, u.BroadcastNotifications, u.NotificationPaid, u.ID)
	return err
}

func (s *Store) DeleteUser(id int) error {
	var query string
	if s.dbType == "sqlite" {
		query = "DELETE FROM users WHERE id = ?"
	} else {
		query = "DELETE FROM users WHERE id = $1"
	}

	_, err := s.db.Exec(query, id)
	return err
}
