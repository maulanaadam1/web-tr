package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"strings"
	"web-tr/internal/config"
	"web-tr/internal/db"
	"web-tr/internal/models"
	"web-tr/internal/snapshot"
	"web-tr/internal/stream"
	"web-tr/internal/sysinfo"
	"web-tr/internal/timelapse"
)

// Session Management
type Session struct {
	UserID           int
	Username         string
	Role             string
	SubscriptionPlan string
	EnableSupport    bool
	PublicToken      string
	Expiry           time.Time
	SubExpiry        time.Time
	TrialClaimed     bool
}

var (
	activeSessions    = make(map[string]Session)
	sessionMutex      sync.Mutex
	sessionCookieName = "webtr_session"
	go2rtcProxy       *httputil.ReverseProxy
	globalStore       *db.Store
)

type contextKey string
const sessionContextKey = contextKey("session")

// CSV Helper Functions
func splitLines(s string) []string {
	var lines []string
	var current string
	for _, ch := range s {
		if ch == '\n' || ch == '\r' {
			if current != "" {
				lines = append(lines, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func splitCSVLine(line string) []string {
	var parts []string
	var current string
	inQuotes := false

	for _, ch := range line {
		if ch == '"' {
			inQuotes = !inQuotes
		} else if ch == ',' && !inQuotes {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(ch)
		}
	}
	parts = append(parts, current)
	return parts
}

func trimString(s string) string {
	// Remove spaces, quotes, and newlines
	result := ""
	for _, ch := range s {
		if ch != ' ' && ch != '\t' && ch != '"' && ch != '\'' && ch != '\r' && ch != '\n' {
			result += string(ch)
		} else if len(result) > 0 && ch == ' ' {
			// Keep internal spaces
			if result[len(result)-1] != ' ' {
				result += " "
			}
		}
	}
	// Trim trailing space
	for len(result) > 0 && result[len(result)-1] == ' ' {
		result = result[:len(result)-1]
	}
	return result
}

func generateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func isUUID(s string) bool {
	return strings.Count(s, "-") == 4 && len(s) >= 32
}

func startsWithString(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}

func proxyToGo2RTC(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	// Allow specific stream endpoints
	allowed := false
	if path == "/rtc/api/ws" || path == "/rtc/api/webrtc" || strings.HasPrefix(path, "/api/stream.") || strings.HasPrefix(path, "/rtc/stream.html") {
		allowed = true
	}
	if strings.HasPrefix(path, "/rtc/api/stream.") || strings.HasPrefix(path, "/rtc/api/frame.") {
		allowed = true
	}

	// Explicitly block sensitive endpoints
	if path == "/rtc/api/streams" || path == "/rtc/api/config" || path == "/rtc/api/models" {
		allowed = false
	}
	
	// Block dashboard
	if path == "/rtc/" || path == "/rtc" {
		allowed = false
	}

	if !allowed {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	targetURL := "http://localhost:1984" + r.URL.RequestURI()
	log.Printf("[Proxy] Request: %s -> %s\n", r.URL.Path, targetURL)

	req, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Copy headers
	for name, values := range r.Header {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Go2RTC Proxy Error: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for name, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func syncStreamToGo2RTC(nodeAPIUrl, name, streamUrl string, isDelete bool) error {
	if nodeAPIUrl == "" {
		nodeAPIUrl = "http://localhost:1984/api/streams"
	}
	method := http.MethodPut
	if isDelete {
		method = http.MethodDelete
	}

	reqUrl := fmt.Sprintf("%s?name=%s&src=%s", nodeAPIUrl, url.QueryEscape(name), url.QueryEscape(streamUrl))
	if isDelete {
		reqUrl = fmt.Sprintf("%s?src=%s", nodeAPIUrl, url.QueryEscape(name))
	}

	req, err := http.NewRequest(method, reqUrl, nil)
	if err != nil {
		return err
	}

	// Add Basic Auth – Matches go2rtc.yaml credentials
	req.SetBasicAuth("admin", "admin123")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("Sync stream %s to %s response: %s (Status: %d)", name, nodeAPIUrl, string(body), resp.StatusCode)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("node api returned status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// MediaMTX API Integration (Port 9997 is the default MediaMTX API port)
func syncStreamToMediaMTX(name, streamUrl string, isDelete bool) error {
	apiHost := "http://localhost:9997/v3/paths/add/" + url.PathEscape(name)
	method := http.MethodPost

	if isDelete {
		apiHost = "http://localhost:9997/v3/paths/delete/" + url.PathEscape(name)
	}

	payload := fmt.Sprintf(`{"source":"%s"}`, streamUrl)
	if isDelete {
		payload = ""
	}

	req, err := http.NewRequest(method, apiHost, strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err // Note: this will fail until MediaMTX is installed on the VPS
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("MediaMTX API error %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func generateSessionToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func sessionAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 0) Special Test Mode Exception (No user/pass required, limited by duration elsewhere)
		if r.URL.Query().Get("test") == "true" {
			ctx := context.WithValue(r.Context(), sessionContextKey, Session{
				UserID:           0, // System/Test user
				Username:         "test_user",
				Role:             "user",
				SubscriptionPlan: "Trial",
				Expiry:           time.Now().Add(1 * time.Hour),
			})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// 1) Gateway / API Basic Auth Fallback
		username, password, hasBasic := r.BasicAuth()
		if hasBasic {
			user, err := globalStore.GetUserByUsername(username)
			if err == nil && user != nil {
				hash := db.HashPassword(password, user.Salt)
				if hash == user.PasswordHash && user.IsActive {
					session := Session{
						UserID:           user.ID,
						Username:         user.Username,
						Role:             user.Role,
						SubscriptionPlan: user.SubscriptionPlan,
						EnableSupport:    user.EnableSupport,
						PublicToken:      user.PublicToken,
						Expiry:           time.Now().Add(24 * time.Hour),
						SubExpiry:        user.ExpiresAt,
					}
					ctx := context.WithValue(r.Context(), sessionContextKey, session)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
			http.Error(w, "Unauthorized API Access", http.StatusUnauthorized)
			return
		}

		// 2) Standard Web Cookie Auth
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		sessionMutex.Lock()
		session, ok := activeSessions[cookie.Value]
		sessionMutex.Unlock()

		if !ok || time.Now().After(session.Expiry) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// Refresh from DB to catch real-time plan/role upgrades
		dbUser, _ := globalStore.GetUserByID(session.UserID)
		if dbUser != nil {
			session.SubscriptionPlan = dbUser.SubscriptionPlan
			session.Role = dbUser.Role
			session.SubExpiry = dbUser.ExpiresAt
			session.TrialClaimed = dbUser.TrialClaimed
			
			// Optional: Update the memory cache too
			sessionMutex.Lock()
			activeSessions[cookie.Value] = session
			sessionMutex.Unlock()
		}

		ctx := context.WithValue(r.Context(), sessionContextKey, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func basicAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username, password, _ := r.BasicAuth()
		adminUser := os.Getenv("ADMIN_USER")
		adminPass := os.Getenv("ADMIN_PASS")
		if adminUser == "" { adminUser = "admin" }
		if adminPass == "" { adminPass = "admin123" }

		if username != adminUser || password != adminPass {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	}
}

func initGo2RTCProxy() {
	target, _ := url.Parse("http://localhost:1984")
	adminUser := os.Getenv("ADMIN_USER")
	adminPass := os.Getenv("ADMIN_PASS")
	if adminUser == "" { adminUser = "admin" }
	if adminPass == "" { adminPass = "admin123" }

	go2rtcProxy = &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			path := strings.TrimPrefix(req.URL.Path, "/rtc")

			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = path
			req.Host = target.Host
			req.Header.Del("Origin")
			req.SetBasicAuth(adminUser, adminPass)
		},
	}
}

func generateTestToken(src, expires string) string {
	h := sha256.New()
	// Using a dedicated secret for test tokens
	h.Write([]byte(src + expires + "RTSP2GO_TEST_SECRET_KEY_2024"))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func secureRTCProxyHandler(w http.ResponseWriter, r *http.Request) {
	// Security: Block access to the root of the RTC proxy to hide the dashboard
	// but allow stream-related files and necessary API endpoints.
	path := r.URL.Path
	if path == "/rtc/" || path == "/rtc" {
		http.Error(w, "Access Denied: The dashboard is restricted.", http.StatusForbidden)
		return
	}

	allowed := false

	// Allow specific API sub-paths needed for streaming
	if path == "/rtc/api/ws" || path == "/rtc/api/webrtc" {
		allowed = true
	}
	if strings.HasPrefix(path, "/rtc/api/stream.") || strings.HasPrefix(path, "/rtc/api/frame.") {
		allowed = true
	}

	// Explicitly block sensitive endpoints
	if path == "/rtc/api/streams" || path == "/rtc/api/config" || path == "/rtc/api/models" {
		http.Error(w, "Access Denied: API endpoint restricted.", http.StatusForbidden)
		return
	}

	// Allow stream.html player page
	if strings.HasPrefix(path, "/rtc/stream.html") {
		allowed = true
	}

	// Allow all static assets (.js, .css, .ico, .png, .svg, .wasm, etc.) needed by the player
	// This avoids whack-a-mole every time go2rtc adds a new dependency
	staticExts := []string{".js", ".css", ".ico", ".png", ".svg", ".wasm", ".map"}
	for _, ext := range staticExts {
		if strings.HasSuffix(path, ext) {
			allowed = true
			break
		}
	}

	// Check for direct RTSP source and enforce expiration
	src := r.URL.Query().Get("src")
	if strings.HasPrefix(src, "rtsp://") {
		// SECURITY FIX: The token/expires check should ONLY be enforced on the initial stream.html load.
		// Sub-requests (like /rtc/api/webrtc?src=rtsp://...) made by Go2RTC's JS don't carry the token.
		if strings.HasPrefix(path, "/rtc/stream.html") {
			expiresStr := r.URL.Query().Get("expires")
			token := r.URL.Query().Get("token")
			
			if expiresStr == "" || token == "" {
				http.Error(w, "Access Denied: Public test stream requires a valid token.", http.StatusForbidden)
				return
			}
			
			expires, err := strconv.ParseInt(expiresStr, 10, 64)
			if err != nil || time.Now().Unix() > expires {
				http.Error(w, "Access Denied: This test stream link has expired (1 hour limit).", http.StatusForbidden)
				return
			}
			
			expected := generateTestToken(src, expiresStr)
			if token != expected {
				http.Error(w, "Access Denied: Invalid test token.", http.StatusForbidden)
				return
			}
		}
		
		// If it's an RTSP source being requested (either player or API), we allow it
		// because the initial entry point (stream.html) was already checked above.
		allowed = true
	}

	if !allowed {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	// If allowed, pass to the real proxy
	go2rtcProxy.ServeHTTP(w, r)
}

func main() {
	// Setup
	os.MkdirAll("data", 0755)
	
	prelaunch := os.Getenv("APP_PRELAUNCH") == "true"
	hideSignup := os.Getenv("HIDE_SIGNUP") == "true"
	hideDocs := os.Getenv("HIDE_DOCS") == "true"
	hidePricing := os.Getenv("HIDE_PRICING") == "true"
	skipLanding := os.Getenv("SKIP_LANDING") == "true" || os.Getenv("HIDE_LANDING_PAGE") == "true"
	cwd, _ := os.Getwd()
	log.Printf("Starting RTSP2go. Working Directory: %s", cwd)
	log.Printf("[Config] HIDE_PAYMENT: %v", strings.ToLower(os.Getenv("HIDE_PAYMENT")) == "true" || os.Getenv("HIDE_PAYMENT") == "1")

	cfgPath := filepath.Join("data", "go2rtc.yaml")
	absCfgPath, _ := filepath.Abs(cfgPath)
	log.Printf("Loading Go2RTC config from: %s", absCfgPath)
	cfgMgr := config.NewConfigManager(cfgPath)

	log.Println("Initializing Stream Manager...")
	streamMgr := stream.NewManager(cfgMgr)

	// DB Setup
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "data/web-tr.db"
	} else if strings.HasPrefix(dbURL, "file:") {
		// Ensure relative file paths go into the 'data/' volume for persistence
		path := strings.TrimPrefix(dbURL, "file:")
		if !filepath.IsAbs(path) && !strings.Contains(path, "/") && !strings.Contains(path, "\\") {
			dbURL = "file:data/" + path
			log.Printf("Persistence Warning: Redirecting relative DB path '%s' to 'data/%s' for volume safety.", path, path)
		}
	}
	
	// If it's a file DSN, log the absolute path for mount debugging
	dsn := dbURL
	dsn = strings.TrimPrefix(dsn, "file:")
	if !strings.HasPrefix(dsn, "postgres://") {
		absDB, _ := filepath.Abs(dsn)
		log.Printf("Using SQLite Database at: %s", absDB)
	}

	store, err := db.NewStore(dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	streamMgr.Store = store
	globalStore = store

	// Ensure there is at least one admin user
	users, _ := store.GetAllUsers()
	if len(users) == 0 {
		defaultUser := os.Getenv("ADMIN_USER")
		defaultPass := os.Getenv("ADMIN_PASS")
		if defaultUser == "" { defaultUser = "admin" }
		if defaultPass == "" { defaultPass = "admin123" }
		log.Printf("No users found. Creating default admin: %s", defaultUser)
		store.CreateUser(defaultUser, defaultPass, "admin")
	}

	// Ensure config exists
	if err := streamMgr.EnsureConfig(); err != nil {
		log.Fatalf("Failed to ensure config: %v", err)
	}

	getStreamInfos := func() []snapshot.StreamInfo {
		var infos []snapshot.StreamInfo
		streams, err := streamMgr.GetStreams()
		if err == nil {
			for _, s := range streams {
				infos = append(infos, snapshot.StreamInfo{Name: s.Name, URL: s.URL})
			}
		}
		return infos
	}

	// Initialize and start Snapshot Manager early so handlers can use it
	// Tries Go2RTC API first, falls back to ffmpeg if Go2RTC fails
	snapshotMgr := snapshot.NewManager(15*time.Minute, filepath.Join("data", "snapshots"), "http://localhost:1984")
	snapshotMgr.Start(getStreamInfos)
	defer snapshotMgr.Stop()

	// Start Heartbeat Monitor (Fast TCP check every 30s)
	streamMgr.StartHeartbeat(context.Background(), 30*time.Second)

	// Initial Sync
	log.Println("Syncing all streams to Go2RTC...")
	if err := streamMgr.Start(); err != nil {
		log.Printf("Error starting go2rtc: %v", err)
		log.Println("Ensure go2rtc (or .exe) is in the current directory or PATH.")
	}

	// Start Engine
	go func() {
		log.Println("Starting go2rtc...")
		// Give Go2RTC a moment to start, then sync all streams
		// This ensures that even if the config file was wiped or we are using DB mode,
		// Go2RTC gets populated with the correct streams.
		time.Sleep(2 * time.Second)
		log.Println("Syncing all streams to Go2RTC...")

		streams, err := streamMgr.GetStreams()
		if err != nil {
			log.Printf("Failed to get streams for sync: %v", err)
		} else {
			for _, s := range streams {
				// Only sync go2rtc backend streams
				if s.Backend == "" || s.Backend == "go2rtc" {
					nodeURL := ""
					if s.NodeID > 0 {
						if n, _ := globalStore.GetNodeByID(s.NodeID); n != nil {
							nodeURL = n.URL
						}
					}
					if err := syncStreamToGo2RTC(nodeURL, s.Name, s.URL, false); err != nil {
						log.Printf("Failed to sync stream %s: %v", s.Name, err)
					} else {
						log.Printf("Synced stream %s to %s", s.Name, nodeURL)
					}
				}
			}
		}
	}()
	defer streamMgr.Stop()

	// Public Share Page
	http.HandleFunc("/share/", func(w http.ResponseWriter, r *http.Request) {
		streamName := strings.TrimPrefix(r.URL.Path, "/share/")
		if streamName == "" {
			streamName = r.URL.Query().Get("stream")
		}
		if streamName == "" {
			http.Error(w, "Stream name is required", http.StatusBadRequest)
			return
		}

		tmpl, err := template.ParseFiles("web/templates/player.html")
		if err != nil {
			log.Printf("Error parsing player template: %v", err)
			http.Error(w, "Template Error", http.StatusInternalServerError)
			return
		}

		// We pass the name. The template will handle the hostname logic via JS.
		tmpl.Execute(w, map[string]interface{}{
			"Name": streamName,
		})
	})

	// HTTP handlers

	initGo2RTCProxy()

	// HTTP handlers
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	http.Handle("/rtc/", http.HandlerFunc(secureRTCProxyHandler)) // Admin and Public access (handled by Director)
	http.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// Legal Pages
	http.HandleFunc("/privacy", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("web/templates/privacy.html")
		if err != nil { http.Error(w, err.Error(), http.StatusInternalServerError); return }
		tmpl.Execute(w, nil)
	})
	http.HandleFunc("/terms", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("web/templates/terms.html")
		if err != nil { http.Error(w, err.Error(), http.StatusInternalServerError); return }
		tmpl.Execute(w, nil)
	})
	http.HandleFunc("/refund", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("web/templates/refund.html")
		if err != nil { http.Error(w, err.Error(), http.StatusInternalServerError); return }
		tmpl.Execute(w, nil)
	})

	http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if (prelaunch || hideSignup) && r.URL.Query().Get("tab") == "signup" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodGet {
			// If already logged in, redirect to admin
			cookie, err := r.Cookie(sessionCookieName)
			if err == nil {
				sessionMutex.Lock()
				_, ok := activeSessions[cookie.Value]
				sessionMutex.Unlock()
				if ok {
					http.Redirect(w, r, "/admin", http.StatusSeeOther)
					return
				}
			}

			tmpl, err := template.ParseFiles("web/templates/login.html")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			errorMsg := r.URL.Query().Get("error")
			successMsg := r.URL.Query().Get("success")
			mode := r.URL.Query().Get("mode") // "register" or empty
			tmpl.Execute(w, map[string]interface{}{
				"Error":       errorMsg,
				"Success":     successMsg,
				"Mode":        mode,
				"HideSignup":  prelaunch || hideSignup,
				"SkipLanding": skipLanding,
				"HidePayment": strings.ToLower(os.Getenv("HIDE_PAYMENT")) == "true" || os.Getenv("HIDE_PAYMENT") == "1",
			})
			return
		}

		if r.Method == http.MethodPost {
			user := r.FormValue("username")
			pass := r.FormValue("password")

			// Check DB for user
			dbUser, err := store.GetUserByUsername(user)
			if err != nil || dbUser == nil {
				http.Redirect(w, r, "/login?error=Invalid+username+or+password", http.StatusSeeOther)
				return
			}

			// Verify Hash (Salted SHA256)
			expectedHash := db.HashPassword(pass, dbUser.Salt)
			if expectedHash == dbUser.PasswordHash {
				token := generateSessionToken()
				expiry := time.Now().Add(24 * time.Hour)

				sessionMutex.Lock()
				activeSessions[token] = Session{
					UserID:           dbUser.ID,
					Username:         dbUser.Username,
					Role:             dbUser.Role,
					SubscriptionPlan: dbUser.SubscriptionPlan,
					EnableSupport:    dbUser.EnableSupport,
					PublicToken:      dbUser.PublicToken,
					Expiry:           expiry,
					SubExpiry:        dbUser.ExpiresAt,
					TrialClaimed:     dbUser.TrialClaimed,
				}
				sessionMutex.Unlock()

				http.SetCookie(w, &http.Cookie{
					Name:     sessionCookieName,
					Value:    token,
					Path:     "/",
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode,
					MaxAge:   86400,
				})
				http.Redirect(w, r, "/admin", http.StatusSeeOther)
				return
			}

			http.Redirect(w, r, "/login?error=Invalid+username+or+password", http.StatusSeeOther)
		}
	})

	http.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err == nil && cookie.Value != "" {
			sessionMutex.Lock()
			delete(activeSessions, cookie.Value)
			sessionMutex.Unlock()
		}

		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	http.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		if prelaunch || hideSignup {
			http.Error(w, "Signup is disabled", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost {
			username := r.FormValue("username")
			password := r.FormValue("password")
			fullName := r.FormValue("full_name")
			email := r.FormValue("email")
			whatsapp := r.FormValue("whatsapp")

			if username == "" || password == "" || fullName == "" || email == "" {
				http.Redirect(w, r, "/login?error=Username,+password,+full+name,+and+email+are+required&mode=register", http.StatusSeeOther)
				return
			}

			// Check if user exists
			existing, _ := store.GetUserByUsername(username)
			if existing != nil {
				http.Redirect(w, r, "/login?error=Username+already+exists&mode=register", http.StatusSeeOther)
				return
			}

			newUser := models.User{
				Username:         username,
				Role:             "user",
				IsActive:         true,
				SubscriptionPlan: "Free", // Start as free until payment confirms
				FullName:         fullName,
				Email:            email,
				Whatsapp:         whatsapp,
			}

			if err := store.CreateUserFull(newUser, password); err != nil {
				log.Printf("Registration error: %v", err)
				http.Redirect(w, r, "/login?error=Failed+to+register+user&mode=register", http.StatusSeeOther)
				return
			}

			user, _ := store.GetUserByUsername(username)
			if user == nil {
				http.Redirect(w, r, "/login?success=Registration+successful.+Please+log+in.", http.StatusSeeOther)
				return
			}

			// Auto Login
			token := generateSessionToken()
			expiry := time.Now().Add(24 * time.Hour)
			sessionMutex.Lock()
			activeSessions[token] = Session{
				UserID:           user.ID,
				Username:         user.Username,
				Role:             user.Role,
				SubscriptionPlan: user.SubscriptionPlan,
				SubExpiry:        user.ExpiresAt,
				TrialClaimed:     user.TrialClaimed,
				Expiry:           expiry,
			}
			sessionMutex.Unlock()

			cookie := http.Cookie{
				Name:     "session_token",
				Value:    token,
				Expires:  expiry,
				HttpOnly: true,
				Path:     "/",
				SameSite: http.SameSiteLaxMode,
			}
			http.SetCookie(w, &cookie)

			planName := r.FormValue("plan")
			hidePayment := strings.ToLower(os.Getenv("HIDE_PAYMENT")) == "true" || os.Getenv("HIDE_PAYMENT") == "1"
			selectedPlan, _ := store.GetPlanByName(planName)

			if !hidePayment && planName != "Free" && planName != "" && selectedPlan != nil && selectedPlan.IsActive {
				ipaymuVA := os.Getenv("IPAYMU_VA")
				ipaymuKey := os.Getenv("IPAYMU_API_KEY")
				production := os.Getenv("IPAYMU_PRODUCTION") == "true"
				appURL := os.Getenv("APP_URL")
				if appURL == "" {
					appURL = "https://localhost"
				}
				if ipaymuVA != "" && ipaymuKey != "" {
					refBytes := make([]byte, 6)
					rand.Read(refBytes)
					refID := fmt.Sprintf("R2G-%d-%s", user.ID, hex.EncodeToString(refBytes))
					
					store.CreateOrder(refID, user.ID, selectedPlan.Name, selectedPlan.Price)
					
					payURL, sessionID, err := createIPPayment(
						ipaymuVA, ipaymuKey, production,
						selectedPlan.Label, int64(selectedPlan.Price), refID,
						user.FullName, user.Email,
						appURL+"/payment/success",
						appURL+"/payment/cancel",
						appURL+"/api/payment/callback",
					)
					if err == nil {
						store.UpdateOrderPaymentURL(refID, payURL, sessionID)
						http.Redirect(w, r, payURL, http.StatusSeeOther)
						return
					} else {
						log.Printf("[Payment] Auto-redirect failed: %v", err)
					}
				}
			}

			// Normal redirect to dashboard
			http.Redirect(w, r, "/admin", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})

	// --- User Management API ---
	http.HandleFunc("/api/users", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		sess := r.Context().Value(sessionContextKey).(Session)
		if sess.Role != "admin" {
			http.Error(w, "Forbidden: Only admins can manage users", http.StatusForbidden)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodGet {
			users, err := store.GetAllUsers()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			
			allStreams, _ := streamMgr.GetStreams()
			
			type UserWithStats struct {
				models.User
				TotalCameras   int `json:"total_cameras"`
				OnlineCameras  int `json:"online_cameras"`
				OfflineCameras int `json:"offline_cameras"`
			}
			
			var result []UserWithStats
			for _, u := range users {
				stats := UserWithStats{User: u}
				for _, s := range allStreams {
					if s.UserID == u.ID {
						stats.TotalCameras++
						if streamMgr.GetOnlineStatus(s.Name) {
							stats.OnlineCameras++
						} else {
							stats.OfflineCameras++
						}
					}
				}
				result = append(result, stats)
			}
			
			json.NewEncoder(w).Encode(result)
			return
		}

		if r.Method == http.MethodPost {
			var req struct {
				models.User
				Password string `json:"password"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if req.Username == "" || req.Password == "" {
				http.Error(w, "Username and password required", http.StatusBadRequest)
				return
			}
			if err := store.CreateUserFull(req.User, req.Password); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusCreated)
			return
		}

		if r.Method == http.MethodDelete {
			idStr := r.URL.Query().Get("id")
			var id int
			fmt.Sscanf(idStr, "%d", &id)
			if err := store.DeleteUser(id); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == http.MethodPut {
			idStr := r.URL.Query().Get("id")
			var id int
			fmt.Sscanf(idStr, "%d", &id)
			
			var req struct {
				models.User
				NewPassword     string `json:"newPassword"`
				CurrentPassword string `json:"currentPassword"`
				UpdatePass      bool   `json:"update_pass"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			req.User.ID = id

			if err := store.UpdateUserFull(req.User); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			if req.UpdatePass && req.NewPassword != "" {
				// Note: In an admin context, we typically allow reset without current password
				// but we could verify CurrentPassword here if the user is editing themselves.
				if err := store.UpdateUserPassword(id, req.NewPassword); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			w.WriteHeader(http.StatusOK)
			return
		}
	}))

	http.HandleFunc("/api/users/me", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		sess := r.Context().Value(sessionContextKey).(Session)

		if r.Method == http.MethodGet {
			user, err := store.GetUserByID(sess.UserID)
			if err != nil || user == nil {
				http.Error(w, "Not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(user)
			return
		}

		if r.Method == http.MethodPut {
			var req map[string]string
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "Invalid body", http.StatusBadRequest)
				return
			}

			user, err := store.GetUserByID(sess.UserID)
			if err != nil || user == nil {
				http.Error(w, "User not found", http.StatusNotFound)
				return
			}

			if pwd, ok := req["password"]; ok {
				if len(pwd) < 6 {
					http.Error(w, "Password minimal 6 karakter", http.StatusBadRequest)
					return
				}
				if err := store.UpdateUserPassword(user.ID, pwd); err != nil {
					http.Error(w, "Gagal mengubah password", http.StatusInternalServerError)
					return
				}
			}

			if wa, ok := req["whatsapp"]; ok {
				user.Whatsapp = wa
				if err := store.UpdateUserFull(*user); err != nil {
					http.Error(w, "Gagal mengupdate WhatsApp", http.StatusInternalServerError)
					return
				}
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"message": "Profile updated successfully"})
			return
		}
	}))

	http.HandleFunc("/api/users/token", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		idStr := r.URL.Query().Get("id")
		var targetID int
		fmt.Sscanf(idStr, "%d", &targetID)

		sess := r.Context().Value(sessionContextKey).(Session)
		if sess.Role != "admin" && sess.UserID != targetID {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// Generate random token
		b := make([]byte, 16)
		rand.Read(b)
		token := hex.EncodeToString(b)

		if err := store.UpdateUserPublicToken(targetID, token); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"public_token": token})
	}))

	http.HandleFunc("/api/sysinfo", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			stats := sysinfo.GetStats()
			
			// Count only streams visible to this user
			sess := r.Context().Value(sessionContextKey).(Session)
			allStoreStreams, _ := streamMgr.GetStreams()
			visibleStreams := getUserVisibleStreams(sess, allStoreStreams, store)
			stats.StreamCount = len(visibleStreams)

			// Detailed counts
			active := 0
			disabled := 0
			for _, s := range visibleStreams {
				if s.Enabled != false {
					active++
				} else {
					disabled++
				}
			}
			stats.ActiveStreams = active
			stats.DisabledStreams = disabled

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(stats)
			return
		}
	}))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}

		if prelaunch {
			csMode := os.Getenv("COMING_SOON_MODE")
			templateName := "web/templates/coming_soon_full.html"
			if csMode == "compact" {
				templateName = "web/templates/coming_soon_compact.html"
			}
			tmpl, err := template.ParseFiles(templateName)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			tmpl.Execute(w, nil)
			return
		}

		if skipLanding {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		tmpl, err := template.ParseFiles("web/templates/landing.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		tmpl.Execute(w, map[string]interface{}{
			"HideDocs":    prelaunch || hideDocs,
			"HideSignup":  prelaunch || hideSignup,
			"HidePricing": hidePricing,
		})
	})

	http.HandleFunc("/api/interest", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var data struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		if data.Email == "" {
			http.Error(w, "Email required", http.StatusBadRequest)
			return
		}
		if err := store.AddInterest(data.Email); err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	// Stateless Test Token Generation with Logging
	http.HandleFunc("/api/public/test-token", func(w http.ResponseWriter, r *http.Request) {
		src := r.URL.Query().Get("src")
		if src == "" {
			http.Error(w, "Source is required", http.StatusBadRequest)
			return
		}
		
		// Log the test attempt
		ip := r.Header.Get("X-Forwarded-For")
		if ip == "" { ip = r.RemoteAddr }
		ua := r.UserAgent()
		_ = store.AddTestLog(src, ip, ua)

		expires := time.Now().Add(1 * time.Hour).Unix()
		expiresStr := strconv.FormatInt(expires, 10)
		token := generateTestToken(src, expiresStr)
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"expires": expires,
			"token":   token,
		})
	})

	// Admin-only Log Viewer
	http.HandleFunc("/api/admin/test-logs", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		sess := r.Context().Value(sessionContextKey).(Session)
		if sess.Role != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		if r.Method == http.MethodGet {
			logs, err := store.GetTestLogs()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(logs)
			return
		}
	}))

	// Admin-only Waiting List (Pre-launch interests)
	http.HandleFunc("/api/admin/interests", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		sess := r.Context().Value(sessionContextKey).(Session)
		if sess.Role != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		if r.Method == http.MethodGet {
			interests, err := store.GetInterests()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(interests)
			return
		}
	}))

	http.HandleFunc("/docs/gateway", func(w http.ResponseWriter, r *http.Request) {
		if prelaunch || hideDocs {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		tmpl, err := template.ParseFiles("web/templates/gateway_docs.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		tmpl.Execute(w, nil)
	})

	adminHandler := sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("web/templates/index.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		streams, err := streamMgr.GetStreams()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		sess := r.Context().Value(sessionContextKey).(Session)
		streams = getUserVisibleStreams(sess, streams, store)

		log.Printf("Rendering dashboard view with %d streams", len(streams))
		
		daysLeft := -1
		if !sess.SubExpiry.IsZero() {
			diff := time.Until(sess.SubExpiry)
			daysLeft = int(diff.Hours() / 24)
			// If it's expiring today, it might be 0.
			if diff > 0 && daysLeft == 0 {
				// We keep it 0 to indicate "Today/Less than 24h"
			}
		}

		tmpl.Execute(w, map[string]interface{}{
			"Streams":  streams,
			"Session":     sess,
			"Now":         time.Now(),
			"DaysLeft":    daysLeft,
			"HidePayment": strings.ToLower(os.Getenv("HIDE_PAYMENT")) == "true" || os.Getenv("HIDE_PAYMENT") == "1",
		})
	})

	http.HandleFunc("/commandcenter", adminHandler)
	http.HandleFunc("/admin", adminHandler)

	http.HandleFunc("/view/", func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.URL.Path, "/view/")
		if token == "" {
			http.Error(w, "Token required", http.StatusBadRequest)
			return
		}

		user, err := store.GetUserByPublicToken(token)
		if err != nil || user == nil {
			http.Error(w, "Invalid or expired public link", http.StatusNotFound)
			return
		}

		tmpl, err := template.ParseFiles("web/templates/public.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		allStreams, _ := streamMgr.GetStreams()
		
		// Use existing helper to handle Role-based and Support-based visibility
		tempSess := Session{
			UserID: user.ID,
			Role:   user.Role,
		}
		filtered := getUserVisibleStreams(tempSess, allStreams, store)

		tmpl.Execute(w, map[string]interface{}{
			"Streams": filtered,
			"User":    user,
		})
	})

	http.HandleFunc("/api/streams", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		sess := r.Context().Value(sessionContextKey).(Session)

		if r.Method == http.MethodGet {
			streams, err := streamMgr.GetStreams()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			
			streams = getUserVisibleStreams(sess, streams, store)
			
			// Build response with online status
			type StreamWithStatus struct {
				models.Stream
				Online bool `json:"online"`
			}
			var response []StreamWithStatus
			for _, s := range streams {
				response = append(response, StreamWithStatus{
					Stream: s,
					Online: streamMgr.GetOnlineStatus(s.Name),
				})
			}
			
			json.NewEncoder(w).Encode(response)
			return
		}

		if r.Method == http.MethodPost {
			var req struct {
				Name         string  `json:"name"`
				DisplayName  string  `json:"display_name"`
				URL          string  `json:"url"`
				Backend      string  `json:"backend,omitempty"`
				Lat          float64 `json:"lat"`
				Lng          float64 `json:"lng"`
				DisableAudio *bool   `json:"disable_audio,omitempty"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			// Default to go2rtc if not specified
			if req.Backend == "" {
				req.Backend = "go2rtc"
			}

			// --- Select Target Node (Dedicated OR Load Balanced) ---
			ownerUser, _ := globalStore.GetUserByID(sess.UserID)

			// Check license expiration
			if sess.UserID != 0 && ownerUser != nil && !ownerUser.ExpiresAt.IsZero() && ownerUser.ExpiresAt.Before(time.Now()) {
				http.Error(w, "Your subscription has expired. Please renew your license.", http.StatusPaymentRequired)
				return
			}

			// Enforce subscription quota limits for non-admins (Skip for Test User)
			if sess.Role != "admin" && sess.UserID != 0 {
				limit := 2 // default Free (Trial)
				if sess.SubscriptionPlan == "Basic" { limit = 4 }
				if sess.SubscriptionPlan == "Premium" { limit = 8 }
				if sess.SubscriptionPlan == "Advance" { limit = 16 }
				if sess.SubscriptionPlan == "Enterprise" { limit = 9999 }
				
				all, _ := streamMgr.GetStreams()
				myStreams := getUserVisibleStreams(sess, all, store)
				if len(myStreams) >= limit {
					http.Error(w, fmt.Sprintf("Upgrade plan to add more cameras. Current limit is %d for %s plan.", limit, sess.SubscriptionPlan), http.StatusForbidden)
					return
				}
			}

			// Special Handling: If Test User (UserID 0), allow overwriting to prevent "duplicate name" errors
			if sess.UserID == 0 {
				// Try to delete existing first if it's the test user
				_ = store.RemoveStream(req.Name) 
			}

			disableAudio := true // Default to true as requested
			if req.DisableAudio != nil {
				disableAudio = *req.DisableAudio
			}

			// --- Select Target Node (Dedicated OR Load Balanced) ---
			var targetNode *models.Node
			
			// Check if owner has dedicated node
			if ownerUser != nil && ownerUser.DedicatedNodeID > 0 {
				targetNode, _ = globalStore.GetNodeByID(ownerUser.DedicatedNodeID)
			}

			if targetNode == nil {
				targetNode, _ = globalStore.GetLeastLoadedNode()
			}

			targetNodeID := 1
			nodeAPI := ""
			if targetNode != nil {
				targetNodeID = targetNode.ID
				nodeAPI = targetNode.URL
			}

			if err := globalStore.AddStream(models.Stream{
				Name: req.Name,
				DisplayName: req.DisplayName,
				URL: req.URL,
				Backend: req.Backend,
				Lat: req.Lat,
				Lng: req.Lng,
				Enabled: true,
				UserID: sess.UserID,
				IsPublic: sess.UserID == 0,
				DisableAudio: disableAudio,
				NodeID: targetNodeID,
			}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			
			streamMgr.SyncFromDB()
			time.Sleep(500 * time.Millisecond)

			// Route stream creation to the correct node
			if req.Backend == "go2rtc" || req.Backend == "ffmpeg" {
				urlToSync := req.URL
				if req.Backend == "ffmpeg" {
					urlToSync = "ffmpeg:" + req.URL + "#video=h264#hardware"
				}
				if disableAudio && !strings.Contains(urlToSync, "#") {
					urlToSync += "#video"
				}
				if err := syncStreamToGo2RTC(nodeAPI, req.Name, urlToSync, false); err != nil {
					log.Printf("Failed to sync stream to go2rtc: %v", err)
				}
			}

			w.WriteHeader(http.StatusCreated)
			return
		}

		if r.Method == http.MethodPut {
			var req struct {
				Name         string  `json:"name"`
				DisplayName  string  `json:"display_name"`
				URL          string  `json:"url"`
				Backend      string  `json:"backend,omitempty"`
				OriginalName string  `json:"originalName"`
				Lat          float64 `json:"lat"`
				Lng          float64 `json:"lng"`
				Enabled      bool    `json:"enabled"`
				DisableAudio bool    `json:"disable_audio"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			req.Name = strings.TrimSpace(req.Name)
			req.OriginalName = strings.TrimSpace(req.OriginalName)
			if req.OriginalName == "" {
				req.OriginalName = req.Name
			}

			// Default to go2rtc if not specified
			if req.Backend == "" {
				req.Backend = "go2rtc"
			}

			log.Printf("[API] Updating stream: OriginalName='%s', NewName='%s', Lat=%f, Lng=%f", req.OriginalName, req.Name, req.Lat, req.Lng)

			if err := streamMgr.UpdateStream(req.OriginalName, req.Name, req.DisplayName, req.URL, req.Lat, req.Lng, req.Enabled, sess.UserID, req.DisableAudio); err != nil {
				log.Printf("[API] Update failed: %v", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// Handle Sync Logic
			if req.Backend == "go2rtc" || req.Backend == "ffmpeg" {
				urlToSync := req.URL
				if req.Backend == "ffmpeg" {
					urlToSync = "ffmpeg:" + req.URL + "#video=h264#hardware"
				}
				// If name changed, delete old
				if req.OriginalName != "" && req.OriginalName != req.Name {
					oldNodeURL := ""
					if existingOld, _ := streamMgr.GetStream(req.OriginalName); existingOld != nil && existingOld.NodeID > 0 {
						if n, _ := globalStore.GetNodeByID(existingOld.NodeID); n != nil {
							oldNodeURL = n.URL
						}
					}
					syncStreamToGo2RTC(oldNodeURL, req.OriginalName, "", true)
				}

				// Resolve target Node API for the current/new name
				nodeURL := ""
				if existing, _ := streamMgr.GetStream(req.Name); existing != nil && existing.NodeID > 0 {
					if n, _ := globalStore.GetNodeByID(existing.NodeID); n != nil {
						nodeURL = n.URL
					}
				}

				if req.DisableAudio && !strings.Contains(urlToSync, "#") {
					urlToSync += "#video"
				}
				if err := syncStreamToGo2RTC(nodeURL, req.Name, urlToSync, false); err != nil {
					log.Printf("Failed to update stream in go2rtc: %v", err)
				}
			} else if req.Backend == "mediamtx" {
				syncStreamToMediaMTX(req.OriginalName, "", true) // Delete old
				syncStreamToMediaMTX(req.Name, req.URL, false)   // Add new
			}

			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == http.MethodDelete {
			// Handle bulk delete: DELETE /api/streams?all=true
			if r.URL.Query().Get("all") == "true" {
				if err := streamMgr.ClearAllStreams(); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				// Also sync/reload Go2RTC globally to clear all active streams
				// We can just rely on the watch config or we can restart the go2rtc process
				w.WriteHeader(http.StatusOK)
				return
			}

			name := r.URL.Query().Get("name")
			if name == "" {
				http.Error(w, "name is required", http.StatusBadRequest)
				return
			}
			
			// Get stream info before deletion to know which backend to sync
			streams, _ := streamMgr.GetStreams()
			var stream models.Stream
			for _, s := range streams {
				if s.Name == name {
					stream = s
					break
				}
			}

			if err := streamMgr.RemoveStream(name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			if stream.Backend == "go2rtc" || stream.Backend == "ffmpeg" {
				nodeURL := ""
				if stream.NodeID > 0 {
					if n, _ := globalStore.GetNodeByID(stream.NodeID); n != nil {
						nodeURL = n.URL
					}
				}
				if err := syncStreamToGo2RTC(nodeURL, stream.Name, "", true); err != nil {
					log.Printf("Failed to delete stream from go2rtc: %v", err)
				}
			} else if stream.Backend == "mediamtx" {
				if err := syncStreamToMediaMTX(stream.Name, "", true); err != nil {
					log.Printf("Failed to delete stream from MediaMTX: %v", err)
				}
			}

			w.WriteHeader(http.StatusOK)
			return
		}
	}))

	// CSV Import endpoint for bulk adding streams
	http.HandleFunc("/api/streams/import", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		sess := r.Context().Value(sessionContextKey).(Session)
		canImport := sess.Role == "admin" || sess.SubscriptionPlan == "Premium" || sess.SubscriptionPlan == "Advance" || sess.SubscriptionPlan == "Enterprise"
		if !canImport {
			http.Error(w, "Import feature is only available for Premium and Advance plans", http.StatusForbidden)
			return
		}

		// Parse multipart form (10MB max)
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		var content []byte
		if rawCSV := r.FormValue("raw_csv"); rawCSV != "" {
			content = []byte(rawCSV)
		} else {
			file, _, err := r.FormFile("file")
			if err != nil {
				http.Error(w, "No file uploaded or raw text provided", http.StatusBadRequest)
				return
			}
			defer file.Close()

			content, err = io.ReadAll(file)
			if err != nil {
				http.Error(w, "Failed to read file", http.StatusInternalServerError)
				return
			}
		}

		// Parse CSV - simple line-by-line parsing
		lines := splitLines(string(content))
		successCount := 0
		failCount := 0
		errors := []string{}

		for i, line := range lines {
			lineNum := i + 1
			line = trimString(line)

			// Skip empty lines, comments
			if line == "" || startsWithString(line, "#") {
				continue
			}

			// Split by comma
			parts := splitCSVLine(line)

			// Simple header detection
			if lineNum == 1 && len(parts) > 0 && strings.ToLower(trimString(parts[0])) == "name" {
				continue
			}
			if len(parts) < 2 {
				failCount++
				errors = append(errors, fmt.Sprintf("Row %d: invalid format (expected: name,url,lat,lng)", lineNum))
				continue
			}

			name := trimString(parts[0])
			streamURL := trimString(parts[1])
			
			var lat, lng float64
			if len(parts) >= 4 {
				fmt.Sscanf(trimString(parts[2]), "%f", &lat)
				fmt.Sscanf(trimString(parts[3]), "%f", &lng)
			}

			if name == "" || streamURL == "" {
				failCount++
				errors = append(errors, fmt.Sprintf("Row %d: empty name or URL", lineNum))
				continue
			}

			// Get session for user ownership
			sess, _ := r.Context().Value(sessionContextKey).(Session)

			// ID Internal harus UUID, Display Name pake nama dari CSV
			internalName := name
			if !isUUID(name) {
				internalName = generateUUID()
			}

			// Add stream (default to go2rtc for CSV imports, auto-mute default)
			if err := streamMgr.AddStream(internalName, name, streamURL, "go2rtc", lat, lng, true, sess.UserID, true); err != nil {
				failCount++
				errors = append(errors, fmt.Sprintf("Row %d (%s): %v", lineNum, name, err))
				continue
			}

			// Sync with Go2RTC (forced mute for CSV)
			urlToSync := streamURL
			if !strings.Contains(urlToSync, "#") {
				urlToSync += "#video"
			}
			if err := syncStreamToGo2RTC("", internalName, urlToSync, false); err != nil {
				log.Printf("Failed to sync stream %s to go2rtc: %v", internalName, err)
			}

			successCount++
		}

		// Return result
		result := map[string]interface{}{
			"success": successCount,
			"failed":  failCount,
			"errors":  errors,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))

	http.HandleFunc("/api/gateway/check", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		sess := r.Context().Value(sessionContextKey).(Session)
		
		limit := 2 // default Free
		duration := 0 // 0 means unlimited
		
		if sess.SubscriptionPlan == "Basic" { limit = 4 }
		if sess.SubscriptionPlan == "Premium" { limit = 8 }
		if sess.SubscriptionPlan == "Advance" { limit = 16 }
		if sess.SubscriptionPlan == "Enterprise" { limit = 9999 }
		
		isTrial := sess.SubscriptionPlan == "Free" || sess.SubscriptionPlan == "Trial"
		if isTrial {
			duration = 60 // 60 minutes for trial
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"username":      sess.Username,
			"plan":          sess.SubscriptionPlan,
			"max_cameras":   limit,
			"limit_minutes": duration,
			"is_trial":      isTrial,
		})
	}))

	http.HandleFunc("/api/streams/export", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		streams, err := streamMgr.GetStreams()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		selectedNames := r.URL.Query().Get("names")
		nameSet := make(map[string]bool)
		if selectedNames != "" {
			for _, n := range strings.Split(selectedNames, ",") {
				nameSet[strings.TrimSpace(n)] = true
			}
		}

		// Prepare CSV output
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment;filename=cctv_export.csv")

		// Write header
		w.Write([]byte("name,url,lat,lng\n"))

		for _, s := range streams {
			if len(nameSet) > 0 && !nameSet[s.Name] {
				continue
			}
			line := fmt.Sprintf("\"%s\",\"%s\",%f,%f\n", s.DisplayName, s.URL, s.Lat, s.Lng)
			w.Write([]byte(line))
		}
	}))

	http.HandleFunc("/api/streams/bulk", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Action string   `json:"action"` // "delete", "enable", "disable"
			Names  []string `json:"names"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		successCount := 0
		for _, name := range req.Names {
			var err error
			switch req.Action {
			case "delete":
				err = streamMgr.RemoveStream(name)
				if err == nil {
					nodeURL := ""
					if stream, _ := streamMgr.GetStream(name); stream != nil && stream.NodeID > 0 {
						if n, _ := globalStore.GetNodeByID(stream.NodeID); n != nil {
							nodeURL = n.URL
						}
					}
					syncStreamToGo2RTC(nodeURL, name, "", true)
				}
			case "enable":
				err = streamMgr.SetStreamStatus(name, true)
			case "disable":
				err = streamMgr.SetStreamStatus(name, false)
			}
			if err == nil {
				successCount++
			} else {
				log.Printf("Bulk action '%s' failed on '%s': %v", req.Action, name, err)
			}
		}

		// Sync logic for enable/disable
		if req.Action == "enable" || req.Action == "disable" {
			for _, name := range req.Names {
				var stream *models.Stream
				all, _ := streamMgr.GetStreams()
				for i := range all {
					if all[i].Name == name {
						stream = &all[i]
						break
					}
				}
				if stream == nil { continue }

				nodeURL := ""
				if stream.NodeID > 0 {
					if n, _ := globalStore.GetNodeByID(stream.NodeID); n != nil {
						nodeURL = n.URL
					}
				}

				if req.Action == "disable" {
					syncStreamToGo2RTC(nodeURL, name, "", true) // Delete from node memory
				} else {
					// Enable: Re-sync to Node
					syncStreamToGo2RTC(nodeURL, stream.Name, stream.URL, false)
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": successCount})
	}))

	http.HandleFunc("/api/admin/nodes", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		sess, _ := r.Context().Value(sessionContextKey).(Session)
		if sess.Role != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		switch r.Method {
		case http.MethodGet:
			nodes, err := globalStore.GetNodes()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(nodes)
		case http.MethodPost:
			var n models.Node
			if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := globalStore.AddNode(n); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusCreated)
		case http.MethodPut:
			var n models.Node
			if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := globalStore.UpdateNode(n); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			idStr := r.URL.Query().Get("id")
			id, _ := strconv.Atoi(idStr)
			if err := globalStore.DeleteNode(id); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	// --- License Manager APIs ---
	http.HandleFunc("/api/admin/licenses", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		sess := r.Context().Value(sessionContextKey).(Session)
		if sess.Role != "admin" {
			http.Error(w, "Unauthorized", http.StatusForbidden)
			return
		}

		if r.Method == http.MethodGet {
			lics, err := globalStore.GetAllLicenses()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(lics)
			return
		}

		if r.Method == http.MethodPost {
			var req struct {
				Plan         string `json:"plan"`
				DurationDays int    `json:"duration_days"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			key, err := globalStore.CreateLicense(req.Plan, req.DurationDays)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"key": key})
			return
		}
	}))

	http.HandleFunc("/api/user/redeem", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		sess := r.Context().Value(sessionContextKey).(Session)
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Key string `json:"key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		plan, err := globalStore.RedeemLicense(sess.UserID, req.Key)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		// Update Active Session to reflect new Plan immediately
		cookie, err := r.Cookie(sessionCookieName)
		if err == nil {
			sessionMutex.Lock()
			if s, ok := activeSessions[cookie.Value]; ok {
				// Refresh user from DB to get new Plan and Expiry
				updatedUser, _ := globalStore.GetUserByID(sess.UserID)
				if updatedUser != nil {
					s.SubscriptionPlan = updatedUser.SubscriptionPlan
					s.SubExpiry = updatedUser.ExpiresAt
					activeSessions[cookie.Value] = s
				}
			}
			sessionMutex.Unlock()
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"plan": plan, "message": "License redeemed successfully"})
	}))

	http.HandleFunc("/api/user/claim-trial", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		sess := r.Context().Value(sessionContextKey).(Session)
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		u, err := globalStore.GetUserByID(sess.UserID)
		if err != nil || u == nil {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}

		if u.TrialClaimed {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "You have already claimed your trial"})
			return
		}

		if u.SubscriptionPlan != "Free" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Trial is only for Free members"})
			return
		}

		// Upgrade to Advance for 2 days
		newExpiry := time.Now().AddDate(0, 0, 2)
		u.SubscriptionPlan = "Advance"
		u.ExpiresAt = newExpiry
		u.TrialClaimed = true

		if err := globalStore.UpdateUserFull(*u); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Update Session
		cookie, err := r.Cookie(sessionCookieName)
		if err == nil {
			sessionMutex.Lock()
			if s, ok := activeSessions[cookie.Value]; ok {
				s.SubscriptionPlan = "Advance"
				s.SubExpiry = newExpiry
				activeSessions[cookie.Value] = s
			}
			sessionMutex.Unlock()
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "Trial claimed successfully", "plan": "Advance"})
	}))

	// Register all iPaymu payment gateway routes
	registerPaymentRoutes()

	http.HandleFunc("/api/probe", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		rawUrl := req.URL
		log.Printf("Probing stream: %s (name: %s)", rawUrl, req.Name)

		resolution, rawOutput, err := streamMgr.ProbeStream(rawUrl)
		if err != nil {
			log.Printf("Probe failed: %v", err)
			// Still return 200 but with error info in JSON so frontend can show raw detail
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "failed",
				"error":  err.Error(),
				"raw":    rawOutput,
			})
			return
		}

		if req.Name != "" && resolution != "" && resolution != "Unknown" {
			log.Printf("Updating resolution for %s: %s", req.Name, resolution)
			store.UpdateStreamResolution(req.Name, resolution)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "success",
			"resolution": resolution,
			"raw":        rawOutput,
		})
	}))

	http.HandleFunc("/api/discover", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		log.Println("Starting network discovery...")
		streams, err := streamMgr.DiscoverStreams()
		if err != nil {
			log.Printf("Discovery failed: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("Discovery complete. Found %d streams", len(streams))
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(streams)
	}))

	http.HandleFunc("/api/snapshot", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			name = r.URL.Query().Get("stream")
		}
		if name == "" {
			http.Error(w, "name or stream parameter required", http.StatusBadRequest)
			return
		}

		path := snapshotMgr.GetSnapshotPath(name)
		if path == "" {
			// If not cached, we could return a 404 or a default icon.
			// Let's redirect to a static placeholder, or return 404 so frontend fallback works.
			http.Error(w, "snapshot not available yet", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s.jpg\"", name))

		http.ServeFile(w, r, path)
	})

	http.HandleFunc("/api/webrtc", func(w http.ResponseWriter, r *http.Request) {
		targetURL := "http://localhost:1984" + r.URL.RequestURI()

		// Create proxy request
		proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Copy headers
		for name, values := range r.Header {
			for _, value := range values {
				proxyReq.Header.Add(name, value)
			}
		}

		// Inject Admin Credentials for local Go2RTC interaction
		adminUser := os.Getenv("ADMIN_USER")
		adminPass := os.Getenv("ADMIN_PASS")
		if adminUser == "" {
			adminUser = "admin"
		}
		if adminPass == "" {
			adminPass = "admin123"
		}
		proxyReq.SetBasicAuth(adminUser, adminPass)

		resp, err := http.DefaultClient.Do(proxyReq)
		if err != nil {
			http.Error(w, fmt.Sprintf("Go2RTC Proxy Error: %v", err), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		// Copy response headers
		for name, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}

		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	})

	// HLS & MSE Proxy Handlers
	http.HandleFunc("/api/stream.mp4", proxyToGo2RTC)    // MSE/MP4
	http.HandleFunc("/api/stream.mjpeg", proxyToGo2RTC) // MJPEG

	// Start Server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server listening on http://0.0.0.0:%s", port)

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	// Start Keep-Alive for all streams to ensure they run in background
	/*
		go func() {
			// Wait a bit for Go2RTC to fully start
			time.Sleep(5 * time.Second)

			streams, err := streamMgr.GetStreams()
			if err != nil {
				log.Printf("KeepAlive: Failed to get streams: %v", err)
				return
			}

			log.Printf("Starting KeepAlive for %d streams to ensure background persistence...", len(streams))

			for _, s := range streams {
				go func(streamName string) {
					// We use the MP4 endpoint as a consumer because it's reliable for keeping the stream open
					// This mimics a client watching the stream 24/7
					client := &http.Client{
						Timeout: 0, // No timeout, we want to hold the connection
					}

					// Using the backend-facing URL (Go2RTC direct)
					// We must URL Encode the stream name
					urlStr := fmt.Sprintf("http://localhost:1984/api/stream.mp4?src=%s", url.QueryEscape(streamName))

					for {
						// Exponential backoff or simple delay implementation usually good here,
						// but simple loop with sleep is fine for this utility.

						resp, err := client.Get(urlStr)
						if err != nil {
							// Stream might be down or Go2RTC restarting
							time.Sleep(5 * time.Second)
							continue
						}

						// Copy to discard to keep flow moving but explicitly discard data
						if _, err := io.Copy(io.Discard, resp.Body); err != nil {
							// Stream ended or connection broke
						}
						resp.Body.Close()

						// Just a small sleep before reconnecting
						time.Sleep(2 * time.Second)
					}
				}(s.Name)
			}
		}()
	*/

	// Start Timelapse Manager
	// We need a helper to get stream URL by name for the timelapse worker
	getStreamURL := func(name string) string {
		streams, err := streamMgr.GetStreams()
		if err != nil {
			return ""
		}
		for _, s := range streams {
			if s.Name == name {
				return s.URL
			}
		}
		return ""
	}

	timelapseMgr := timelapse.NewManager(filepath.Join("data", "timelapse.json"))
	if err := timelapseMgr.Load(); err != nil {
		log.Printf("Warning: Failed to load timelapse config: %v", err)
	}
	timelapseMgr.Start(getStreamURL)
	defer timelapseMgr.Stop()

	// API: Get Timelapse Config
	http.HandleFunc("/api/timelapse/config", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			name := r.URL.Query().Get("name")
			if name != "" {
				settings := timelapseMgr.GetSettings(name)
				if settings == nil {
					// Return default disabled
					json.NewEncoder(w).Encode(timelapse.TimelapseSettings{Enabled: false, Interval: 60})
					return
				}
				json.NewEncoder(w).Encode(settings)
				return
			}
			// Return all (map)
			// Not used by frontend yet, but good for debug
			json.NewEncoder(w).Encode(timelapseMgr.Settings)
			return
		}

		if r.Method == http.MethodPost {
			var req struct {
				Name     string `json:"name"`
				Enabled  bool   `json:"enabled"`
				Interval int    `json:"interval"`
				Width    int    `json:"width"`
				Height   int    `json:"height"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			// Save
			settings := timelapse.TimelapseSettings{
				Enabled:  req.Enabled,
				Interval: req.Interval,
				Width:    req.Width,
				Height:   req.Height,
				LastRun:  0, // Reset run on config change? Or keep history?
				// Usually keep history if just changing interval. But simplistic approach is fine.
				// Better: check existing to preserve LastRun
				// For now simple overwrite is safer to trigger capture if interval shortened
			}

			// Preserve LastRun if exists
			if existing := timelapseMgr.GetSettings(req.Name); existing != nil {
				settings.LastRun = existing.LastRun
			}

			if err := timelapseMgr.UpdateSettings(req.Name, settings); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
	}))

	// API: Get Timelapse Files
	http.HandleFunc("/api/timelapse/files", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}

		files, err := timelapseMgr.GetFiles(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(files)
	})

	// API: Delete Timelapse Files
	http.HandleFunc("/api/timelapse/files/delete", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := r.URL.Query().Get("name")
		filename := r.URL.Query().Get("filename") // if "all", delete all

		if name == "" || filename == "" {
			http.Error(w, "name and filename required", http.StatusBadRequest)
			return
		}

		if filename == "all" {
			if err := timelapseMgr.DeleteAll(name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			if err := timelapseMgr.DeleteFile(name, filename); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Serve Timelapse Data Files
	// /start/data/timelapse/...
	// We need to serve the data directory
	http.Handle("/data/", http.StripPrefix("/data/", http.FileServer(http.Dir("data"))))

	// Export Timelapse Video
	http.HandleFunc("/api/timelapse/export", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		start := r.URL.Query().Get("start")
		end := r.URL.Query().Get("end")
		if name == "" || start == "" || end == "" {
			http.Error(w, "Missing name, start, or end parameters", http.StatusBadRequest)
			return
		}

		path, err := timelapseMgr.ExportVideo(name, start, end)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s_timelapse.mp4", name))
		http.ServeFile(w, r, path)
	})

	// Timelapse Viewer Page
	http.HandleFunc("/timelapse", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("web/templates/timelapse.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		tmpl.Execute(w, nil)
	}))

	// =============================================================
	// IoT Bridge v2 (Push Mode) — /api/bridge/{cam_name}
	// Gateway tells us it wants to push a stream.
	// We register it and tell go2rtc to wait for a push.
	// =============================================================
	http.HandleFunc("/api/bridge/", func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimPrefix(r.URL.Path, "/api/bridge/")
		if slug == "" {
			http.Error(w, "camera name required", http.StatusBadRequest)
			return
		}

		// Find if this slug already exists in DB as an internal Name
		existing, _ := globalStore.GetStream(slug)
		camName := slug
		if existing == nil && !isUUID(slug) {
			camName = generateUUID()
		}

		// --- Auth ---
		username, password, hasBasic := r.BasicAuth()
		if !hasBasic {
			username = r.URL.Query().Get("user")
			password = r.URL.Query().Get("pass")
		}
		if username == "" {
			username = r.Header.Get("X-Api-User")
			password = r.Header.Get("X-Api-Pass")
		}
		isTrial := (username == "" && password == "")
		var ownerUserID int
		if !isTrial {
			dbUser, err := globalStore.GetUserByUsername(username)
			if err != nil || dbUser == nil || db.HashPassword(password, dbUser.Salt) != dbUser.PasswordHash || !dbUser.IsActive {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			ownerUserID = dbUser.ID
		} else {
			ownerUserID = 1 // default admin for trial
		}

		displayName := r.Header.Get("X-Display-Name")
		if displayName == "" {
			displayName = slug
		}

		// --- Select Target Node (Dedicated OR Load Balanced) ---
		var targetNode *models.Node
		
		// Fetch Owner Info once
		ownerUser, _ := globalStore.GetUserByID(ownerUserID)

		// Check license expiration
		if ownerUser != nil && !ownerUser.ExpiresAt.IsZero() && ownerUser.ExpiresAt.Before(time.Now()) {
			http.Error(w, "Subscription Expired", http.StatusPaymentRequired)
			return
		}
		
		// Priority 1: User's Dedicated Node
		if ownerUser != nil && ownerUser.DedicatedNodeID > 0 {
			targetNode, _ = globalStore.GetNodeByID(ownerUser.DedicatedNodeID)
			if targetNode != nil {
				log.Printf("[Bridge v2] User %s has dedicated Node %d", ownerUser.Username, targetNode.ID)
			}
		}
		
		// Priority 2: Least Loaded Node
		if targetNode == nil {
			targetNode, _ = globalStore.GetLeastLoadedNode()
		}

		targetNodeID := 1
		nodeAPI := "http://localhost:1984/api/streams"
		nodeIP := "localhost"
		rtspPort := 8554

		if targetNode != nil {
			targetNodeID = targetNode.ID
			nodeAPI = targetNode.URL
			rtspPort = targetNode.RtspPort
			
			// Extract IP from Node URL for the gateway response
			parsedUrl, _ := url.Parse(nodeAPI)
			nodeIP = parsedUrl.Hostname()
			if nodeIP == "" { nodeIP = targetNode.URL } // Fallback
		}

		// --- Register placeholder in the target Node's go2rtc ---
		if err := syncStreamToGo2RTC(nodeAPI, camName, "!", false); err != nil {
			log.Printf("[Bridge v2] node sync error: %v", err)
		}

		// --- Register in DB ---
		existingStreams, _ := streamMgr.GetStreams()
		found := false
		for _, s := range existingStreams {
			if s.Name == camName {
				found = true
				// Update node_id if it changed
				if s.NodeID != targetNodeID {
					s.NodeID = targetNodeID
					globalStore.AddStream(s) // Update
				}
				break
			}
		}
		if !found {
			newStream := models.Stream{
				Name:        camName,
				DisplayName: displayName,
				URL:         "push",
				Backend:     "go2rtc",
				Enabled:     true,
				IsPublic:    true,
				UserID:      ownerUserID,
				NodeID:      targetNodeID,
			}
			globalStore.AddStream(newStream)
		}

		log.Printf("[Bridge v2] Camera '%s' assigned to Node %d (%s)", camName, targetNodeID, nodeIP)
		
		// --- Handle TCP Upgrade (for ESP32 Bridge) ---
		if strings.ToLower(r.Header.Get("Upgrade")) == "rtsp-bridge" {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "webserver doesn't support hijacking", http.StatusInternalServerError)
				return
			}
			conn, bufrw, err := hijacker.Hijack()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer conn.Close()

			// Write 101 Response
			bufrw.WriteString("HTTP/1.1 101 Switching Protocols\r\n")
			bufrw.WriteString("Upgrade: rtsp-bridge\r\n")
			bufrw.WriteString("Connection: Upgrade\r\n")
			bufrw.WriteString(fmt.Sprintf("X-Node-IP: %s\r\n", nodeIP))
			bufrw.WriteString("\r\n")
			bufrw.Flush()

			// Connect to Target Node's RTSP Port (8554)
			targetAddr := fmt.Sprintf("%s:%d", nodeIP, rtspPort)
			if nodeIP == "localhost" {
				targetAddr = fmt.Sprintf("127.0.0.1:%d", rtspPort)
			}
			
			log.Printf("[Bridge v2] Proxying TCP Bridge for %s to %s", camName, targetAddr)
			
			backendConn, err := net.DialTimeout("tcp", targetAddr, 5*time.Second)
			if err != nil {
				log.Printf("[Bridge v2] Failed to dial node backend: %v", err)
				return
			}
			defer backendConn.Close()

			// Handshake for go2rtc push (ANNOUNCE etc will be handled by the client/bridge loop)
			// For raw bridge, we just pipe.
			
			errc := make(chan error, 2)
			cp := func(dst io.Writer, src io.Reader) {
				_, err := io.Copy(dst, src)
				errc <- err
			}
			go cp(backendConn, bufrw)
			go cp(conn, backendConn)
			<-errc
			return
		}

		// --- Default Response (for FFmpeg/Gateway App) ---
		w.Header().Set("X-Node-IP", nodeIP)
		w.Header().Set("X-Node-Port", fmt.Sprintf("%d", rtspPort))
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, "Registered for push mode. Target IP: %s, Port: %d", nodeIP, rtspPort)
	})

	go func() {
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			log.Fatal(err)
		}
	}()

	<-stop
	log.Println("Shutting down server...")
	streamMgr.Stop()
	timelapseMgr.Stop()
}

func getUserVisibleStreams(sess Session, streams []models.Stream, store *db.Store) []models.Stream {
	if sess.Role == "admin" {
		users, _ := store.GetAllUsers()
		supportEnabledMap := make(map[int]bool)
		for _, u := range users {
			// Admin can see cameras if user has explicitly enabled support
			if u.EnableSupport {
				supportEnabledMap[u.ID] = true
			}
		}

		var filtered []models.Stream
		for _, s := range streams {
			// Admin can see: their own, support-enabled users, and test user (UserID 0)
			if s.UserID == sess.UserID || supportEnabledMap[s.UserID] || s.UserID == 0 {
				filtered = append(filtered, s)
			}
		}
		return filtered
	}

	var filtered []models.Stream
	for _, s := range streams {
		if s.UserID == sess.UserID {
			filtered = append(filtered, s)
		}
	}
	return filtered
}
