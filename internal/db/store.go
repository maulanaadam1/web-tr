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

	// Create nodes table for multi-server support
	var nodeQuery string
	if s.dbType == "sqlite" {
		nodeQuery = `
		CREATE TABLE IF NOT EXISTS nodes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			rtsp_port INTEGER DEFAULT 8554,
			secret TEXT,
			is_active BOOLEAN DEFAULT 1,
			location TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`
	} else {
		nodeQuery = `
		CREATE TABLE IF NOT EXISTS nodes (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			rtsp_port INTEGER DEFAULT 8554,
			secret TEXT,
			is_active BOOLEAN DEFAULT TRUE,
			location TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`
	}
	if _, err := s.db.Exec(nodeQuery); err != nil {
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
			"node_id INTEGER DEFAULT 1",   // Added node_id
			"is_public BOOLEAN DEFAULT 1",
			"resolution TEXT DEFAULT ''",
			"display_name TEXT DEFAULT ''",
			"disable_audio BOOLEAN DEFAULT 0",
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
		ADD COLUMN IF NOT EXISTS public_token TEXT DEFAULT '';
		ALTER TABLE streams
		ADD COLUMN IF NOT EXISTS lat DOUBLE PRECISION DEFAULT 0,
		ADD COLUMN IF NOT EXISTS lng DOUBLE PRECISION DEFAULT 0,
		ADD COLUMN IF NOT EXISTS is_enabled BOOLEAN DEFAULT TRUE,
		ADD COLUMN IF NOT EXISTS resolution TEXT DEFAULT '',
		ADD COLUMN IF NOT EXISTS display_name TEXT DEFAULT '',
		ADD COLUMN IF NOT EXISTS disable_audio BOOLEAN DEFAULT FALSE;`
		_, _ = s.db.Exec(alterQuery)
	}

	if err := s.SeedDefaultAdmin(); err != nil {
		log.Printf("Warning: Failed to seed default admin: %v", err)
	}

	s.MigrateOldStreamsToUUID()

	return nil
}

func (s *Store) MigrateOldStreamsToUUID() {
	rows, err := s.db.Query("SELECT id, name FROM streams WHERE name NOT LIKE '%-%-%-%-%'")
	if err != nil {
		return
	}
	defer rows.Close()

	type OldStream struct {
		ID   int
		Name string
	}
	var targets []OldStream
	for rows.Next() {
		var o OldStream
		if err := rows.Scan(&o.ID, &o.Name); err == nil {
			targets = append(targets, o)
		}
	}

	for _, t := range targets {
		newUUID := make([]byte, 16)
		rand.Read(newUUID)
		uuidStr := fmt.Sprintf("%x-%x-%x-%x-%x", newUUID[0:4], newUUID[4:6], newUUID[6:8], newUUID[8:10], newUUID[10:])

		var updateQuery string
		if s.dbType == "sqlite" {
			updateQuery = "UPDATE streams SET display_name = COALESCE(NULLIF(display_name, ''), name), name = ? WHERE id = ?"
		} else {
			updateQuery = "UPDATE streams SET display_name = COALESCE(NULLIF(display_name, ''), name), name = $1 WHERE id = $2"
		}
		
		_, err := s.db.Exec(updateQuery, uuidStr, t.ID)
		if err == nil {
			log.Printf("Database Migration: Migrated legacy stream '%s' to UUID: %s", t.Name, uuidStr)
		} else {
			log.Printf("Database Migration Failed for '%s': %v", t.Name, err)
		}
	}
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
	rows, err := s.db.Query("SELECT name, COALESCE(display_name, name) as display_name, url, COALESCE(backend, 'go2rtc') as backend, COALESCE(lat, 0) as lat, COALESCE(lng, 0) as lng, COALESCE(is_enabled, 1) as is_enabled, COALESCE(user_id, 1) as user_id, COALESCE(node_id, 1) as node_id, COALESCE(is_public, 1) as is_public, COALESCE(resolution, '') as resolution, COALESCE(disable_audio, 0) as disable_audio FROM streams ORDER BY display_name ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var st models.Stream
		if err := rows.Scan(&st.Name, &st.DisplayName, &st.URL, &st.Backend, &st.Lat, &st.Lng, &st.Enabled, &st.UserID, &st.NodeID, &st.IsPublic, &st.Resolution, &st.DisableAudio); err != nil {
			log.Printf("Error scanning row: %v", err)
			continue
		}
		// Default to go2rtc if empty
		if st.Backend == "" {
			st.Backend = "go2rtc"
		}
		if st.DisplayName == "" {
			st.DisplayName = st.Name
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
	if st.DisplayName == "" {
		st.DisplayName = st.Name
	}

	var query string
	// Default to user_id 1 if not set
	if (st.UserID == -1) {
		st.UserID = 1
	}

	if s.dbType == "sqlite" {
		query = "INSERT INTO streams (name, display_name, url, backend, lat, lng, is_enabled, user_id, node_id, is_public, resolution, disable_audio) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (name) DO UPDATE SET display_name = ?, url = ?, backend = ?, lat = ?, lng = ?, is_enabled = ?, user_id = ?, node_id = ?, is_public = ?, resolution = ?, disable_audio = ?"
		_, err := s.db.Exec(query, st.Name, st.DisplayName, st.URL, st.Backend, st.Lat, st.Lng, st.Enabled, st.UserID, st.NodeID, st.IsPublic, st.Resolution, st.DisableAudio, st.DisplayName, st.URL, st.Backend, st.Lat, st.Lng, st.Enabled, st.UserID, st.NodeID, st.IsPublic, st.Resolution, st.DisableAudio)
		return err
	} else {
		query = "INSERT INTO streams (name, display_name, url, backend, lat, lng, is_enabled, user_id, node_id, is_public, resolution, disable_audio) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) ON CONFLICT (name) DO UPDATE SET display_name = $2, url = $3, backend = $4, lat = $5, lng = $6, is_enabled = $7, user_id = $8, node_id = $9, is_public = $10, resolution = $11, disable_audio = $12"
		_, err := s.db.Exec(query, st.Name, st.DisplayName, st.URL, st.Backend, st.Lat, st.Lng, st.Enabled, st.UserID, st.NodeID, st.IsPublic, st.Resolution, st.DisableAudio)
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

func (s *Store) UpdateStream(oldName, newName, displayName, url, backend string, lat, lng float64, enabled bool, userID int, disableAudio bool) error {
	newName = strings.TrimSpace(newName)
	oldName = strings.TrimSpace(oldName)
	if displayName == "" {
		displayName = newName
	}

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
		updateNameQuery = "UPDATE streams SET name = ?, display_name = ?, url = ?, backend = ?, lat = ?, lng = ?, is_enabled = ?, user_id = ?, disable_audio = ? WHERE name = ?"
		updateUrlQuery = "UPDATE streams SET display_name = ?, url = ?, backend = ?, lat = ?, lng = ?, is_enabled = ?, user_id = ?, disable_audio = ? WHERE name = ?"
	} else {
		checkQuery = "SELECT EXISTS(SELECT 1 FROM streams WHERE name = $1)"
		updateNameQuery = "UPDATE streams SET name = $1, display_name = $2, url = $3, backend = $4, lat = $5, lng = $6, is_enabled = $7, user_id = $8, disable_audio = $9 WHERE name = $10"
		updateUrlQuery = "UPDATE streams SET display_name = $1, url = $2, backend = $3, lat = $4, lng = $5, is_enabled = $6, user_id = $7, disable_audio = $8 WHERE name = $9"
	}

	if oldName != newName {
		// Check if new name (UUID/ID) exists. Usually newName == oldName for updates now, unless migrating.
		var exists bool
		err := tx.QueryRow(checkQuery, newName).Scan(&exists)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("stream id '%s' already exists", newName)
		}

		// Update name, display_name, url, backend, and user_id
		_, err = tx.Exec(updateNameQuery, newName, displayName, url, backend, lat, lng, enabled, userID, disableAudio, oldName)
		if err != nil {
			return err
		}
	} else {
		// Just update display_name, url, backend, and user_id
		_, err = tx.Exec(updateUrlQuery, displayName, url, backend, lat, lng, enabled, userID, disableAudio, newName)
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
		query = "INSERT INTO users (username, password_hash, salt, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid, subscription_plan, enable_support, public_token) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	} else {
		query = "INSERT INTO users (username, password_hash, salt, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid, subscription_plan, enable_support, public_token) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)"
	}

	_, err := s.db.Exec(query, u.Username, hash, salt, u.Role, u.FullName, u.Email, u.Whatsapp, u.IsActive, u.BroadcastNotifications, u.NotificationPaid, u.SubscriptionPlan, u.EnableSupport, u.PublicToken)
	return err
}


func (s *Store) CreateUser(username, password, role string) error {
	return s.CreateUserFull(models.User{Username: username, Role: role, IsActive: true}, password)
}

func (s *Store) GetUserByUsername(username string) (*models.User, error) {
	var user models.User
	var query string
	if s.dbType == "sqlite" {
		query = "SELECT id, username, password_hash, salt, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid, COALESCE(subscription_plan, 'Free'), COALESCE(enable_support, 0), COALESCE(public_token, ''), created_at FROM users WHERE username = ?"
	} else {
		query = "SELECT id, username, password_hash, salt, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid, COALESCE(subscription_plan, 'Free'), COALESCE(enable_support, false), COALESCE(public_token, ''), created_at FROM users WHERE username = $1"
	}

	err := s.db.QueryRow(query, username).
		Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Salt, &user.Role, &user.FullName, &user.Email, &user.Whatsapp, &user.IsActive, &user.BroadcastNotifications, &user.NotificationPaid, &user.SubscriptionPlan, &user.EnableSupport, &user.PublicToken, &user.CreatedAt)
	
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // User not found
		}
		return nil, err
	}
	return &user, nil
}

func (s *Store) GetAllUsers() ([]models.User, error) {
	rows, err := s.db.Query("SELECT id, username, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid, COALESCE(subscription_plan, 'Free'), COALESCE(enable_support, 0), COALESCE(public_token, ''), created_at FROM users ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.FullName, &u.Email, &u.Whatsapp, &u.IsActive, &u.BroadcastNotifications, &u.NotificationPaid, &u.SubscriptionPlan, &u.EnableSupport, &u.PublicToken, &u.CreatedAt); err != nil {
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
		query = "UPDATE users SET role = ?, full_name = ?, email = ?, whatsapp = ?, is_active = ?, broadcast_notifications = ?, notification_paid = ?, subscription_plan = ?, enable_support = ?, public_token = ? WHERE id = ?"
	} else {
		query = "UPDATE users SET role = $1, full_name = $2, email = $3, whatsapp = $4, is_active = $5, broadcast_notifications = $6, notification_paid = $7, subscription_plan = $8, enable_support = $9, public_token = $10 WHERE id = $11"
	}

	_, err := s.db.Exec(query, u.Role, u.FullName, u.Email, u.Whatsapp, u.IsActive, u.BroadcastNotifications, u.NotificationPaid, u.SubscriptionPlan, u.EnableSupport, u.PublicToken, u.ID)
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
func (s *Store) AddTestLog(url, ip, ua string) error {
	var query string
	if s.dbType == "sqlite" {
		query = "INSERT INTO test_logs (url, ip_address, user_agent) VALUES (?, ?, ?)"
	} else {
		query = "INSERT INTO test_logs (url, ip_address, user_agent) VALUES ($1, $2, $3)"
	}
	_, err := s.db.Exec(query, url, ip, ua)
	return err
}

func (s *Store) GetTestLogs() ([]models.TestLog, error) {
	query := "SELECT id, url, ip_address, user_agent, created_at FROM test_logs ORDER BY created_at DESC LIMIT 500"
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []models.TestLog
	for rows.Next() {
		var l models.TestLog
		var ip, ua sql.NullString
		if err := rows.Scan(&l.ID, &l.URL, &ip, &ua, &l.CreatedAt); err != nil {
			return nil, err
		}
		l.IP = ip.String
		l.UserAgent = ua.String
		logs = append(logs, l)
	}
	return logs, nil
}
func (s *Store) GetInterests() ([]models.Interest, error) {
	query := "SELECT id, email, created_at FROM interests ORDER BY created_at DESC"
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.Interest
	for rows.Next() {
		var i models.Interest
		if err := rows.Scan(&i.ID, &i.Email, &i.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, i)
	}
	return result, nil
}

// --- Node Management Methods ---

func (s *Store) GetNodes() ([]models.Node, error) {
	rows, err := s.db.Query("SELECT id, name, url, rtsp_port, COALESCE(secret, ''), is_active, COALESCE(location, ''), created_at FROM nodes ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []models.Node
	for rows.Next() {
		var n models.Node
		if err := rows.Scan(&n.ID, &n.Name, &n.URL, &n.RtspPort, &n.Secret, &n.IsActive, &n.Location, &n.CreatedAt); err != nil {
			log.Printf("Error scanning node row: %v", err)
			continue
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

func (s *Store) AddNode(n models.Node) error {
	var query string
	if s.dbType == "sqlite" {
		query = "INSERT INTO nodes (name, url, rtsp_port, secret, is_active, location) VALUES (?, ?, ?, ?, ?, ?)"
	} else {
		query = "INSERT INTO nodes (name, url, rtsp_port, secret, is_active, location) VALUES ($1, $2, $3, $4, $5, $6)"
	}
	_, err := s.db.Exec(query, n.Name, n.URL, n.RtspPort, n.Secret, n.IsActive, n.Location)
	return err
}

func (s *Store) UpdateNode(n models.Node) error {
	var query string
	if s.dbType == "sqlite" {
		query = "UPDATE nodes SET name = ?, url = ?, rtsp_port = ?, secret = ?, is_active = ?, location = ? WHERE id = ?"
	} else {
		query = "UPDATE nodes SET name = $1, url = $2, rtsp_port = $3, secret = $4, is_active = $5, location = $6 WHERE id = $7"
	}
	_, err := s.db.Exec(query, n.Name, n.URL, n.RtspPort, n.Secret, n.IsActive, n.Location, n.ID)
	return err
}

func (s *Store) DeleteNode(id int) error {
	var query string
	if s.dbType == "sqlite" {
		query = "DELETE FROM nodes WHERE id = ?"
	} else {
		query = "DELETE FROM nodes WHERE id = $1"
	}
	_, err := s.db.Exec(query, id)
	return err
}
