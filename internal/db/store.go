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
	"time"
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

	// Migration: Add dedicated_node_id to users if not exists
	s.db.Exec("ALTER TABLE users ADD COLUMN dedicated_node_id INTEGER DEFAULT 0")
	s.db.Exec("ALTER TABLE users ADD COLUMN public_token TEXT") // Ensure public_token also exists

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
			"trial_claimed BOOLEAN DEFAULT 0",
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
		ADD COLUMN IF NOT EXISTS trial_claimed BOOLEAN DEFAULT FALSE,
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

	s.MigrateOldStreamsToUUID() // Re-enabled to ensure internal identifiers are UUIDs while UI uses DisplayName

	// Migration: Add expires_at to users
	s.db.Exec("ALTER TABLE users ADD COLUMN expires_at TIMESTAMP")

	// Create licenses table
	licenseQuery := `
	CREATE TABLE IF NOT EXISTS licenses (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		key TEXT UNIQUE NOT NULL,
		plan TEXT NOT NULL,
		duration_days INTEGER NOT NULL,
		is_used BOOLEAN DEFAULT 0,
		used_by_user_id INTEGER DEFAULT 0,
		used_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`
	if s.dbType != "sqlite" {
		licenseQuery = `
		CREATE TABLE IF NOT EXISTS licenses (
			id SERIAL PRIMARY KEY,
			key TEXT UNIQUE NOT NULL,
			plan TEXT NOT NULL,
			duration_days INTEGER NOT NULL,
			is_used BOOLEAN DEFAULT FALSE,
			used_by_user_id INTEGER DEFAULT 0,
			used_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`
	}
	if _, err := s.db.Exec(licenseQuery); err != nil {
		return err
	}

	// Create plans table (pricing config)
	plansQuery := `
	CREATE TABLE IF NOT EXISTS plans (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT UNIQUE NOT NULL,
		label TEXT NOT NULL,
		price INTEGER NOT NULL,
		duration_days INTEGER NOT NULL DEFAULT 30,
		max_cameras INTEGER NOT NULL DEFAULT 4,
		is_active BOOLEAN DEFAULT 1,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`
	if s.dbType != "sqlite" {
		plansQuery = `
		CREATE TABLE IF NOT EXISTS plans (
			id SERIAL PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			label TEXT NOT NULL,
			price INTEGER NOT NULL,
			duration_days INTEGER NOT NULL DEFAULT 30,
			max_cameras INTEGER NOT NULL DEFAULT 4,
			is_active BOOLEAN DEFAULT TRUE,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`
	}
	if _, err := s.db.Exec(plansQuery); err != nil {
		return err
	}

	// Seed default plans if empty
	s.seedDefaultPlans()

	// Create orders table (payment tracking)
	ordersQuery := `
	CREATE TABLE IF NOT EXISTS orders (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		reference_id TEXT UNIQUE NOT NULL,
		user_id INTEGER NOT NULL,
		plan_name TEXT NOT NULL,
		amount INTEGER NOT NULL,
		status TEXT DEFAULT 'pending',
		payment_url TEXT DEFAULT '',
		session_id TEXT DEFAULT '',
		paid_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`
	if s.dbType != "sqlite" {
		ordersQuery = `
		CREATE TABLE IF NOT EXISTS orders (
			id SERIAL PRIMARY KEY,
			reference_id TEXT UNIQUE NOT NULL,
			user_id INTEGER NOT NULL,
			plan_name TEXT NOT NULL,
			amount INTEGER NOT NULL,
			status TEXT DEFAULT 'pending',
			payment_url TEXT DEFAULT '',
			session_id TEXT DEFAULT '',
			paid_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`
	}
	if _, err := s.db.Exec(ordersQuery); err != nil {
		return err
	}

	return nil
}

func (s *Store) seedDefaultPlans() {
	defaults := []struct {
		Name     string
		Label    string
		Price    int
		Days     int
		MaxCams  int
	}{
		{"Basic", "Basic Plan", 15000, 30, 4},
		{"Premium", "Premium Plan", 35000, 30, 8},
		{"Advance", "Advance Plan", 70000, 30, 16},
	}
	for _, p := range defaults {
		if s.dbType == "sqlite" {
			s.db.Exec("INSERT OR IGNORE INTO plans (name, label, price, duration_days, max_cameras) VALUES (?, ?, ?, ?, ?)",
				p.Name, p.Label, p.Price, p.Days, p.MaxCams)
		} else {
			s.db.Exec("INSERT INTO plans (name, label, price, duration_days, max_cameras) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (name) DO NOTHING",
				p.Name, p.Label, p.Price, p.Days, p.MaxCams)
		}
	}
}

// --- Plan Management ---

func (s *Store) GetAllPlans() ([]models.Plan, error) {
	rows, err := s.db.Query("SELECT id, name, label, price, duration_days, max_cameras, is_active, updated_at FROM plans ORDER BY price ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var plans []models.Plan
	for rows.Next() {
		var p models.Plan
		if err := rows.Scan(&p.ID, &p.Name, &p.Label, &p.Price, &p.DurationDays, &p.MaxCameras, &p.IsActive, &p.UpdatedAt); err != nil {
			continue
		}
		plans = append(plans, p)
	}
	return plans, nil
}

func (s *Store) GetPlanByName(name string) (*models.Plan, error) {
	var p models.Plan
	var query string
	if s.dbType == "sqlite" {
		query = "SELECT id, name, label, price, duration_days, max_cameras, is_active, updated_at FROM plans WHERE name = ?"
	} else {
		query = "SELECT id, name, label, price, duration_days, max_cameras, is_active, updated_at FROM plans WHERE name = $1"
	}
	err := s.db.QueryRow(query, name).Scan(&p.ID, &p.Name, &p.Label, &p.Price, &p.DurationDays, &p.MaxCameras, &p.IsActive, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) UpdatePlan(p models.Plan) error {
	var query string
	if s.dbType == "sqlite" {
		query = "UPDATE plans SET label = ?, price = ?, duration_days = ?, max_cameras = ?, is_active = ?, updated_at = CURRENT_TIMESTAMP WHERE name = ?"
	} else {
		query = "UPDATE plans SET label = $1, price = $2, duration_days = $3, max_cameras = $4, is_active = $5, updated_at = NOW() WHERE name = $6"
	}
	_, err := s.db.Exec(query, p.Label, p.Price, p.DurationDays, p.MaxCameras, p.IsActive, p.Name)
	return err
}

// --- Order Management ---

func (s *Store) CreateOrder(refID string, userID int, planName string, amount int) error {
	var query string
	if s.dbType == "sqlite" {
		query = "INSERT INTO orders (reference_id, user_id, plan_name, amount) VALUES (?, ?, ?, ?)"
	} else {
		query = "INSERT INTO orders (reference_id, user_id, plan_name, amount) VALUES ($1, $2, $3, $4)"
	}
	_, err := s.db.Exec(query, refID, userID, planName, amount)
	return err
}

func (s *Store) UpdateOrderPaymentURL(refID, paymentURL, sessionID string) error {
	var query string
	if s.dbType == "sqlite" {
		query = "UPDATE orders SET payment_url = ?, session_id = ? WHERE reference_id = ?"
	} else {
		query = "UPDATE orders SET payment_url = $1, session_id = $2 WHERE reference_id = $3"
	}
	_, err := s.db.Exec(query, paymentURL, sessionID, refID)
	return err
}

func (s *Store) GetOrderByRef(refID string) (*models.Order, error) {
	var o models.Order
	var query string
	if s.dbType == "sqlite" {
		query = "SELECT id, reference_id, user_id, plan_name, amount, status, payment_url, session_id, created_at FROM orders WHERE reference_id = ?"
	} else {
		query = "SELECT id, reference_id, user_id, plan_name, amount, status, payment_url, session_id, created_at FROM orders WHERE reference_id = $1"
	}
	err := s.db.QueryRow(query, refID).Scan(&o.ID, &o.ReferenceID, &o.UserID, &o.PlanName, &o.Amount, &o.Status, &o.PaymentURL, &o.SessionID, &o.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (s *Store) MarkOrderPaid(refID string) (*models.Order, error) {
	var query string
	if s.dbType == "sqlite" {
		query = "UPDATE orders SET status = 'paid', paid_at = CURRENT_TIMESTAMP WHERE reference_id = ? AND status = 'pending'"
	} else {
		query = "UPDATE orders SET status = 'paid', paid_at = NOW() WHERE reference_id = $1 AND status = 'pending'"
	}
	res, err := s.db.Exec(query, refID)
	if err != nil {
		return nil, err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return nil, fmt.Errorf("order already processed or not found")
	}
	return s.GetOrderByRef(refID)
}

func (s *Store) GetRecentOrders(limit int) ([]models.Order, error) {
	rows, err := s.db.Query("SELECT id, reference_id, user_id, plan_name, amount, status, payment_url, session_id, created_at FROM orders ORDER BY created_at DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var orders []models.Order
	for rows.Next() {
		var o models.Order
		if err := rows.Scan(&o.ID, &o.ReferenceID, &o.UserID, &o.PlanName, &o.Amount, &o.Status, &o.PaymentURL, &o.SessionID, &o.CreatedAt); err != nil {
			continue
		}
		orders = append(orders, o)
	}
	return orders, nil
}

// GenerateLicenseKey creates a R2G-PLAN-XXXX-XXXX key
func GenerateLicenseKey(plan string) string {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // Sans O,0,I,1 for readability
	prefix := "R2G"
	planCode := "FREE"
	p := strings.ToUpper(plan)
	if strings.Contains(p, "BASIC") { planCode = "BASE" }
	if strings.Contains(p, "PREMIUM") { planCode = "PREM" }
	if strings.Contains(p, "ADVANCE") { planCode = "ADVN" }
	if strings.Contains(p, "ENTERPRISE") { planCode = "ENTP" }

	b := make([]byte, 8)
	rand.Read(b)
	
	part1 := ""
	part2 := ""
	for i := 0; i < 4; i++ {
		part1 += string(charset[int(b[i])%len(charset)])
		part2 += string(charset[int(b[4+i])%len(charset)])
	}

	return fmt.Sprintf("%s-%s-%s-%s", prefix, planCode, part1, part2)
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

func (s *Store) GetStream(name string) (*models.Stream, error) {
	var st models.Stream
	var query string
	if s.dbType == "sqlite" {
		query = "SELECT name, COALESCE(display_name, name) as display_name, url, COALESCE(backend, 'go2rtc') as backend, COALESCE(lat, 0) as lat, COALESCE(lng, 0) as lng, COALESCE(is_enabled, 1) as is_enabled, COALESCE(user_id, 1) as user_id, COALESCE(node_id, 1) as node_id, COALESCE(is_public, 1) as is_public, COALESCE(resolution, '') as resolution, COALESCE(disable_audio, 0) as disable_audio FROM streams WHERE name = ?"
	} else {
		query = "SELECT name, COALESCE(display_name, name) as display_name, url, COALESCE(backend, 'go2rtc') as backend, COALESCE(lat, 0) as lat, COALESCE(lng, 0) as lng, COALESCE(is_enabled, 1) as is_enabled, COALESCE(user_id, 1) as user_id, COALESCE(node_id, 1) as node_id, COALESCE(is_public, 1) as is_public, COALESCE(resolution, '') as resolution, COALESCE(disable_audio, 0) as disable_audio FROM streams WHERE name = $1"
	}

	err := s.db.QueryRow(query, name).Scan(&st.Name, &st.DisplayName, &st.URL, &st.Backend, &st.Lat, &st.Lng, &st.Enabled, &st.UserID, &st.NodeID, &st.IsPublic, &st.Resolution, &st.DisableAudio)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &st, nil
}

func (s *Store) AddStream(st models.Stream) error {
	// Default to go2rtc if backend not specified
	if st.Backend == "" {
		st.Backend = "go2rtc"
	}
	if st.DisplayName == "" {
		st.DisplayName = st.Name
	} else if st.Name == "" {
		// If DisplayName is provided but Name is not, use DisplayName as Name
		st.Name = st.DisplayName
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
		query = "INSERT INTO users (username, password_hash, salt, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid, subscription_plan, enable_support, public_token, dedicated_node_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	} else {
		query = "INSERT INTO users (username, password_hash, salt, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid, subscription_plan, enable_support, public_token, dedicated_node_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)"
	}

	_, err := s.db.Exec(query, u.Username, hash, salt, u.Role, u.FullName, u.Email, u.Whatsapp, u.IsActive, u.BroadcastNotifications, u.NotificationPaid, u.SubscriptionPlan, u.EnableSupport, u.PublicToken, u.DedicatedNodeID)
	return err
}


func (s *Store) CreateUser(username, password, role string) error {
	return s.CreateUserFull(models.User{Username: username, Role: role, IsActive: true}, password)
}

func (s *Store) GetUserByUsername(username string) (*models.User, error) {
	var user models.User
	var query string
	if s.dbType == "sqlite" {
		query = "SELECT id, username, password_hash, salt, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid, COALESCE(subscription_plan, 'Free'), COALESCE(enable_support, 0), COALESCE(public_token, ''), COALESCE(dedicated_node_id, 0), COALESCE(trial_claimed, 0), expires_at, created_at FROM users WHERE username = ?"
	} else {
		query = "SELECT id, username, password_hash, salt, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid, COALESCE(subscription_plan, 'Free'), COALESCE(enable_support, false), COALESCE(public_token, ''), COALESCE(dedicated_node_id, 0), COALESCE(trial_claimed, false), expires_at, created_at FROM users WHERE username = $1"
	}

	var expiresAt sql.NullTime
	err := s.db.QueryRow(query, username).
		Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Salt, &user.Role, &user.FullName, &user.Email, &user.Whatsapp, &user.IsActive, &user.BroadcastNotifications, &user.NotificationPaid, &user.SubscriptionPlan, &user.EnableSupport, &user.PublicToken, &user.DedicatedNodeID, &user.TrialClaimed, &expiresAt, &user.CreatedAt)
	
	if expiresAt.Valid {
		user.ExpiresAt = expiresAt.Time
	}
	
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // User not found
		}
		return nil, err
	}
	return &user, nil
}

func (s *Store) GetUserByID(id int) (*models.User, error) {
	var user models.User
	var query string
	if s.dbType == "sqlite" {
		query = "SELECT id, username, password_hash, salt, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid, COALESCE(subscription_plan, 'Free'), COALESCE(enable_support, 0), COALESCE(public_token, ''), COALESCE(dedicated_node_id, 0), COALESCE(trial_claimed, 0), expires_at, created_at FROM users WHERE id = ?"
	} else {
		query = "SELECT id, username, password_hash, salt, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid, COALESCE(subscription_plan, 'Free'), COALESCE(enable_support, false), COALESCE(public_token, ''), COALESCE(dedicated_node_id, 0), COALESCE(trial_claimed, false), expires_at, created_at FROM users WHERE id = $1"
	}

	var expiresAt sql.NullTime
	err := s.db.QueryRow(query, id).
		Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Salt, &user.Role, &user.FullName, &user.Email, &user.Whatsapp, &user.IsActive, &user.BroadcastNotifications, &user.NotificationPaid, &user.SubscriptionPlan, &user.EnableSupport, &user.PublicToken, &user.DedicatedNodeID, &user.TrialClaimed, &expiresAt, &user.CreatedAt)
	
	if expiresAt.Valid {
		user.ExpiresAt = expiresAt.Time
	}
	
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // User not found
		}
		return nil, err
	}
	return &user, nil
}

func (s *Store) GetAllUsers() ([]models.User, error) {
	rows, err := s.db.Query("SELECT id, username, role, full_name, email, whatsapp, is_active, broadcast_notifications, notification_paid, COALESCE(subscription_plan, 'Free'), COALESCE(enable_support, 0), COALESCE(public_token, ''), COALESCE(dedicated_node_id, 0), COALESCE(trial_claimed, 0), expires_at, created_at FROM users ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		var expiresAt sql.NullTime
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.FullName, &u.Email, &u.Whatsapp, &u.IsActive, &u.BroadcastNotifications, &u.NotificationPaid, &u.SubscriptionPlan, &u.EnableSupport, &u.PublicToken, &u.DedicatedNodeID, &u.TrialClaimed, &expiresAt, &u.CreatedAt); err != nil {
			log.Printf("Error scanning user row: %v", err)
			continue
		}
		if expiresAt.Valid {
			u.ExpiresAt = expiresAt.Time
		}
		users = append(users, u)
	}
	return users, nil
}

func (s *Store) MarkTrialClaimed(id int) error {
	var query string
	if s.dbType == "sqlite" {
		query = "UPDATE users SET trial_claimed = 1 WHERE id = ?"
	} else {
		query = "UPDATE users SET trial_claimed = TRUE WHERE id = $1"
	}
	_, err := s.db.Exec(query, id)
	return err
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
		query = "UPDATE users SET role = ?, full_name = ?, email = ?, whatsapp = ?, is_active = ?, broadcast_notifications = ?, notification_paid = ?, subscription_plan = ?, enable_support = ?, public_token = ?, dedicated_node_id = ?, trial_claimed = ?, expires_at = ? WHERE id = ?"
	} else {
		query = "UPDATE users SET role = $1, full_name = $2, email = $3, whatsapp = $4, is_active = $5, broadcast_notifications = $6, notification_paid = $7, subscription_plan = $8, enable_support = $9, public_token = $10, dedicated_node_id = $11, trial_claimed = $12, expires_at = $13 WHERE id = $14"
	}

	_, err := s.db.Exec(query, u.Role, u.FullName, u.Email, u.Whatsapp, u.IsActive, u.BroadcastNotifications, u.NotificationPaid, u.SubscriptionPlan, u.EnableSupport, u.PublicToken, u.DedicatedNodeID, u.TrialClaimed, u.ExpiresAt, u.ID)
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

func (s *Store) GetNodeByID(id int) (*models.Node, error) {
	var n models.Node
	var query string
	if s.dbType == "sqlite" {
		query = "SELECT id, name, url, rtsp_port, COALESCE(secret, ''), is_active, COALESCE(location, ''), created_at FROM nodes WHERE id = ?"
	} else {
		query = "SELECT id, name, url, rtsp_port, COALESCE(secret, ''), is_active, COALESCE(location, ''), created_at FROM nodes WHERE id = $1"
	}

	err := s.db.QueryRow(query, id).Scan(&n.ID, &n.Name, &n.URL, &n.RtspPort, &n.Secret, &n.IsActive, &n.Location, &n.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &n, nil
}

func (s *Store) GetLeastLoadedNode() (*models.Node, error) {
	// Query the node with the minimum number of streams assigned
	// We only consider active nodes
	var query string
	if s.dbType == "sqlite" {
		query = `
			SELECT n.id, n.name, n.url, n.rtsp_port, COALESCE(n.secret, ''), n.is_active, COALESCE(n.location, ''), n.created_at
			FROM nodes n
			LEFT JOIN streams s ON n.id = s.node_id
			WHERE n.is_active = 1
			GROUP BY n.id
			ORDER BY COUNT(s.id) ASC
			LIMIT 1`
	} else {
		query = `
			SELECT n.id, n.name, n.url, n.rtsp_port, COALESCE(n.secret, ''), n.is_active, COALESCE(n.location, ''), n.created_at
			FROM nodes n
			LEFT JOIN streams s ON n.id = s.node_id
			WHERE n.is_active = TRUE
			GROUP BY n.id, n.name, n.url, n.rtsp_port, n.secret, n.is_active, n.location, n.created_at
			ORDER BY COUNT(s.id) ASC
			LIMIT 1`
	}

	var n models.Node
	err := s.db.QueryRow(query).Scan(&n.ID, &n.Name, &n.URL, &n.RtspPort, &n.Secret, &n.IsActive, &n.Location, &n.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No active nodes available
		}
		return nil, err
	}
	return &n, nil
}

func (s *Store) CreateLicense(plan string, days int) (string, error) {
	key := GenerateLicenseKey(plan)
	var query string
	if s.dbType == "sqlite" {
		query = "INSERT INTO licenses (key, plan, duration_days) VALUES (?, ?, ?)"
	} else {
		query = "INSERT INTO licenses (key, plan, duration_days) VALUES ($1, $2, $3)"
	}
	_, err := s.db.Exec(query, key, plan, days)
	return key, err
}

func (s *Store) GetAllLicenses() ([]models.License, error) {
	rows, err := s.db.Query("SELECT id, key, plan, duration_days, is_used, used_by_user_id, used_at, created_at FROM licenses ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var licenses []models.License
	for rows.Next() {
		var l models.License
		if err := rows.Scan(&l.ID, &l.Key, &l.Plan, &l.DurationDays, &l.IsUsed, &l.UsedByUserID, &l.UsedAt, &l.CreatedAt); err != nil {
			continue
		}
		licenses = append(licenses, l)
	}
	return licenses, nil
}

func (s *Store) RedeemLicense(userID int, key string) (string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var plan string
	var days int
	var isUsed bool
	var query string
	if s.dbType == "sqlite" {
		query = "SELECT plan, duration_days, is_used FROM licenses WHERE key = ?"
	} else {
		query = "SELECT plan, duration_days, is_used FROM licenses WHERE key = $1"
	}

	err = tx.QueryRow(query, key).Scan(&plan, &days, &isUsed)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("invalid license key")
		}
		return "", err
	}

	if isUsed {
		return "", fmt.Errorf("your redeem keys has been used")
	}

	// Calculate new expiry
	now := time.Now()
	var currentExpiryStr string
	if s.dbType == "sqlite" {
		query = "SELECT COALESCE(expires_at, '1970-01-01 00:00:00') FROM users WHERE id = ?"
	} else {
		query = "SELECT COALESCE(expires_at, '1970-01-01 00:00:00') FROM users WHERE id = $1"
	}
	tx.QueryRow(query, userID).Scan(&currentExpiryStr)

	currentExpiry, _ := time.Parse("2006-01-02 15:04:05", currentExpiryStr)
	if currentExpiry.IsZero() {
		currentExpiry = time.Unix(0, 0)
	}

	newExpiry := now.AddDate(0, 0, days)
	if currentExpiry.After(now) {
		newExpiry = currentExpiry.AddDate(0, 0, days)
	}

	// Update User
	if s.dbType == "sqlite" {
		query = "UPDATE users SET subscription_plan = ?, expires_at = ? WHERE id = ?"
	} else {
		query = "UPDATE users SET subscription_plan = $1, expires_at = $2 WHERE id = $3"
	}
	_, err = tx.Exec(query, plan, newExpiry, userID)
	if err != nil { return "", err }

	// Mark license as used
	if s.dbType == "sqlite" {
		query = "UPDATE licenses SET is_used = 1, used_by_user_id = ?, used_at = ? WHERE key = ?"
	} else {
		query = "UPDATE licenses SET is_used = TRUE, used_by_user_id = $1, used_at = $2 WHERE key = $3"
	}
	_, err = tx.Exec(query, userID, now, key)
	if err != nil { return "", err }

	err = tx.Commit()
	return plan, err
}
