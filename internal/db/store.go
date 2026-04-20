package db

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"os"
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

	// Create interests table for pre-launch
	var interestQuery string
	if s.dbType == "sqlite" {
		interestQuery = `
		CREATE TABLE IF NOT EXISTS interests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT UNIQUE NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`
	} else {
		interestQuery = `
		CREATE TABLE IF NOT EXISTS interests (
			id SERIAL PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`
	}
	if _, err := s.db.Exec(interestQuery); err != nil {
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
			"subscription_plan TEXT DEFAULT 'Free'",
			"enable_support BOOLEAN DEFAULT 0",
			"enable_vpn BOOLEAN DEFAULT 0",
			"vpn_password TEXT DEFAULT ''",
			"public_token TEXT DEFAULT ''",
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
			"user_id INTEGER DEFAULT 1",
			"is_public BOOLEAN DEFAULT 1",
			"resolution TEXT DEFAULT ''",
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
		ADD COLUMN IF NOT EXISTS notification_paid BOOLEAN DEFAULT FALSE,
		ADD COLUMN IF NOT EXISTS enable_support BOOLEAN DEFAULT FALSE,
		ADD COLUMN IF NOT EXISTS enable_vpn BOOLEAN DEFAULT FALSE,
		ADD COLUMN IF NOT EXISTS vpn_password TEXT DEFAULT '',
		ADD COLUMN IF NOT EXISTS public_token TEXT DEFAULT '';
		ALTER TABLE streams
		ADD COLUMN IF NOT EXISTS lat DOUBLE PRECISION DEFAULT 0,
		ADD COLUMN IF NOT EXISTS lng DOUBLE PRECISION DEFAULT 0,
		ADD COLUMN IF NOT EXISTS is_enabled BOOLEAN DEFAULT TRUE,
		ADD COLUMN IF NOT EXISTS resolution TEXT DEFAULT '';`
		_, _ = s.db.Exec(alterQuery)
	}

	if err := s.SeedDefaultAdmin(); err != nil {
		log.Printf("Warning: Failed to seed default admin: %v", err)
	}

	return nil
}

func (s *Store) SeedDefaultAdmin() error {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		return err
	}

	if count > 0 {
		return nil // Admin already exists
	}

	// Use Environment Variables or defaults
	adminUser := os.Getenv("ADMIN_USER")
	adminPass := os.Getenv("ADMIN_PASS")
	if adminUser == "" { adminUser = "admin" }
	if adminPass == "" { adminPass = "admin123" }

	log.Printf("Seeding default admin user: %s", adminUser)
	return s.CreateUserFull(models.User{
		Username: adminUser,
		Role:     "admin",
	}, adminPass)
}

func (s *Store) GetStreams() ([]models.Stream, error) {
	rows, err := s.db.Query("SELECT name, url, COALESCE(backend, 'go2rtc') as backend, COALESCE(lat, 0) as lat, COALESCE(lng, 0) as lng, COALESCE(is_enabled, 1) as is_enabled, COALESCE(user_id, 1) as user_id, COALESCE(is_public, 1) as is_public, COALESCE(resolution, '') as resolution FROM streams ORDER BY name ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var st models.Stream
		if err := rows.Scan(&st.Name, &st.URL, &st.Backend, &st.Lat, &st.Lng, &st.Enabled, &st.UserID, &st.IsPublic, &st.Resolution); err != nil {
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
	// Default to user_id 1 if not set
	if st.UserID == 0 {
		st.UserID = 1
	}

	if s.dbType == "sqlite" {
		query = "INSERT INTO streams (name, url, backend, lat, lng, is_enabled, user_id, is_public) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (name) DO UPDATE SET url = ?, backend = ?, lat = ?, lng = ?, is_enabled = ?, user_id = ?, is_public = ?"
		_, err := s.db.Exec(query, st.Name, st.URL, st.Backend, st.Lat, st.Lng, st.Enabled, st.UserID, st.IsPublic, st.URL, st.Backend, st.Lat, st.Lng, st.Enabled, st.UserID, st.IsPublic)
		return err
	} else {
		query = "INSERT INTO streams (name, url, backend, lat, lng, is_enabled, user_id, is_public) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (name) DO UPDATE SET url = $2, backend = $3, lat = $4, lng = $5, is_enabled = $6, user_id = $7, is_public = $8"
		_, err := s.db.Exec(query, st.Name, st.URL, st.Backend, st.Lat, st.Lng, st.Enabled, st.UserID, st.IsPublic)
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

func (s *Store) UpdateStreamResolution(name string, resolution string) error {
	var query string
	if s.dbType == "sqlite" {
		query = "UPDATE streams SET resolution = ? WHERE name = ?"
	} else {
		query = "UPDATE streams SET resolution = $1 WHERE name = $2"
	}
	_, err := s.db.Exec(query, resolution, name)
	return err
}

func (s *Store) ClearAllStreams() error {
	_, err := s.db.Exec("DELETE FROM streams")
	return err
}

func (s *Store) UpdateStream(oldName, newName, url, backend string, lat, lng float64, enabled bool, userID int) error {
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
		updateNameQuery = "UPDATE streams SET name = ?, url = ?, backend = ?, lat = ?, lng = ?, is_enabled = ?, user_id = ? WHERE name = ?"
		updateUrlQuery = "UPDATE streams SET url = ?, backend = ?, lat = ?, lng = ?, is_enabled = ?, user_id = ? WHERE name = ?"
	} else {
		checkQuery = "SELECT EXISTS(SELECT 1 FROM streams WHERE name = $1)"
		updateNameQuery = "UPDATE streams SET name = $1, url = $2, backend = $3, lat = $4, lng = $5, is_enabled = $6, user_id = $7 WHERE name = $8"
		updateUrlQuery = "UPDATE streams SET url = $1, backend = $2, lat = $3, lng = $4, is_enabled = $5, user_id = $6 WHERE name = $7"
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

		// Update name, url, backend, and user_id
		_, err = tx.Exec(updateNameQuery, newName, url, backend, lat, lng, enabled, userID, oldName)
		if err != nil {
			return err
		}
	} else {
		// Just update url, backend, and user_id
		_, err = tx.Exec(updateUrlQuery, url, backend, lat, lng, enabled, userID, newName)
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

func GenerateVPNPassword() string {
	b := make([]byte, 5) // 10 hex chars
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Store) CreateUserFull(u models.User, password string) error {
	salt := GenerateSalt()
	hash := HashPassword(password, salt)

	if u.SubscriptionPlan == "" {
		u.SubscriptionPlan = "Free"
	}
	if u.VPNPassword == "" {
		u.VPNPassword = GenerateVPNPassword()
	}

	var query string
	if s.dbType == "sqlite" {
		query = "INSERT INTO users (username, password_hash, salt, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid, subscription_plan, enable_support, enable_vpn, vpn_password, public_token) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	} else {
		query = "INSERT INTO users (username, password_hash, salt, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid, subscription_plan, enable_support, enable_vpn, vpn_password, public_token) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)"
	}

	_, err := s.db.Exec(query, u.Username, hash, salt, u.Role, u.FullName, u.Email, u.Whatsapp, u.IsActive, u.BroadcastNotifications, u.NotificationPaid, u.SubscriptionPlan, u.EnableSupport, u.EnableVPN, u.VPNPassword, u.PublicToken)
	if err == nil {
		// Sync with L2TP VPN Server securely
		s.SyncVPNUserToSecrets(u.Username, u.VPNPassword)
	}
	return err
}

// SyncVPNUserToSecrets physically writes or updates the username and password in the Linux L2TP secrets file
func (s *Store) SyncVPNUserToSecrets(username, vpnPass string) {
	// Assumes /etc/ppp is mounted from Host into the Docker Container
	path := "/etc/ppp/chap-secrets"
	content, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Cannot read %s: %v (Is volume mounted?)", path, err)
		return
	}

	lines := strings.Split(string(content), "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), `"`+username+`"`) || strings.HasPrefix(strings.TrimSpace(line), username+" l2tpd") {
			// Update existing
			lines[i] = fmt.Sprintf(`"%s" l2tpd "%s" *`, username, vpnPass)
			found = true
			break
		}
	}

	if !found {
		// Append new
		lines = append(lines, fmt.Sprintf(`"%s" l2tpd "%s" *`, username, vpnPass))
	}

	err = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600)
	if err != nil {
		log.Printf("Failed to sync VPN Secrets to disk: %v", err)
	} else {
		log.Printf("Successfully synced User '%s' to L2TP VPN Config!", username)
	}
}

func (s *Store) CreateUser(username, password, role string) error {
	return s.CreateUserFull(models.User{Username: username, Role: role, IsActive: true}, password)
}

func (s *Store) GetUserByUsername(username string) (*models.User, error) {
	var user models.User
	var query string
	if s.dbType == "sqlite" {
		query = "SELECT id, username, password_hash, salt, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid, COALESCE(subscription_plan, 'Free'), COALESCE(enable_support, 0), COALESCE(enable_vpn, 0), COALESCE(vpn_password, ''), COALESCE(public_token, ''), created_at FROM users WHERE username = ?"
	} else {
		query = "SELECT id, username, password_hash, salt, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid, COALESCE(subscription_plan, 'Free'), COALESCE(enable_support, false), COALESCE(enable_vpn, false), COALESCE(vpn_password, ''), COALESCE(public_token, ''), created_at FROM users WHERE username = $1"
	}

	err := s.db.QueryRow(query, username).
		Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Salt, &user.Role, &user.FullName, &user.Email, &user.Whatsapp, &user.IsActive, &user.BroadcastNotifications, &user.NotificationPaid, &user.SubscriptionPlan, &user.EnableSupport, &user.EnableVPN, &user.VPNPassword, &user.PublicToken, &user.CreatedAt)
	
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // User not found
		}
		return nil, err
	}
	return &user, nil
}

func (s *Store) GetAllUsers() ([]models.User, error) {
	rows, err := s.db.Query("SELECT id, username, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid, COALESCE(subscription_plan, 'Free'), COALESCE(enable_support, 0), COALESCE(enable_vpn, 0), COALESCE(vpn_password, ''), COALESCE(public_token, ''), created_at FROM users ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.FullName, &u.Email, &u.Whatsapp, &u.IsActive, &u.BroadcastNotifications, &u.NotificationPaid, &u.SubscriptionPlan, &u.EnableSupport, &u.EnableVPN, &u.VPNPassword, &u.PublicToken, &u.CreatedAt); err != nil {
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
		query = "UPDATE users SET role = ?, full_name = ?, email = ?, whatsapp = ?, is_active = ?, broadcast_notifications = ?, notification_paid = ?, subscription_plan = ?, enable_support = ?, enable_vpn = ?, vpn_password = ?, public_token = ? WHERE id = ?"
	} else {
		query = "UPDATE users SET role = $1, full_name = $2, email = $3, whatsapp = $4, is_active = $5, broadcast_notifications = $6, notification_paid = $7, subscription_plan = $8, enable_support = $9, enable_vpn = $10, vpn_password = $11, public_token = $12 WHERE id = $13"
	}

	_, err := s.db.Exec(query, u.Role, u.FullName, u.Email, u.Whatsapp, u.IsActive, u.BroadcastNotifications, u.NotificationPaid, u.SubscriptionPlan, u.EnableSupport, u.EnableVPN, u.VPNPassword, u.PublicToken, u.ID)
	return err
}

func (s *Store) UpdateUserPublicToken(id int, token string) error {
	var query string
	if s.dbType == "sqlite" {
		query = "UPDATE users SET public_token = ? WHERE id = ?"
	} else {
		query = "UPDATE users SET public_token = $1 WHERE id = $2"
	}
	_, err := s.db.Exec(query, token, id)
	return err
}

func (s *Store) GetUserByPublicToken(token string) (*models.User, error) {
	if token == "" {
		return nil, fmt.Errorf("empty token")
	}
	var user models.User
	var query string
	if s.dbType == "sqlite" {
		query = "SELECT id, username, role, full_name, email, whatsapp, is_active, COALESCE(subscription_plan, 'Free'), created_at FROM users WHERE public_token = ? AND is_active = 1"
	} else {
		query = "SELECT id, username, role, full_name, email, whatsapp, is_active, COALESCE(subscription_plan, 'Free'), created_at FROM users WHERE public_token = $1 AND is_active = true"
	}

	err := s.db.QueryRow(query, token).
		Scan(&user.ID, &user.Username, &user.Role, &user.FullName, &user.Email, &user.Whatsapp, &user.IsActive, &user.SubscriptionPlan, &user.CreatedAt)
	
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
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

func (s *Store) AddInterest(email string) error {
	var query string
	if s.dbType == "sqlite" {
		query = "INSERT INTO interests (email) VALUES (?) ON CONFLICT(email) DO NOTHING"
	} else {
		query = "INSERT INTO interests (email) VALUES ($1) ON CONFLICT(email) DO NOTHING"
	}
	_, err := s.db.Exec(query, email)
	return err
}
