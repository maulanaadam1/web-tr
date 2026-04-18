package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
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
	Expiry           time.Time
}

var (
	activeSessions    = make(map[string]Session)
	sessionMutex      sync.Mutex
	sessionCookieName = "webtr_session"
	go2rtcProxy       *httputil.ReverseProxy
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

func startsWithString(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}

func proxyToGo2RTC(w http.ResponseWriter, r *http.Request) {
	// Security: Block access to the root of the RTC proxy to hide the dashboard
	// but allow stream-related files and necessary API endpoints.
	path := r.URL.Path
	if path == "/rtc/" || path == "/rtc" {
		http.Error(w, "Access Denied: You do not have permission to view the dashboard.", http.StatusForbidden)
		return
	}

	allowed := false
	// Safe paths for public viewing and underlying WebRTC/MSE mechanics
	safePaths := []string{
		"/rtc/stream.html",
		"/rtc/api/ws",      // WebSockets for signaling
		"/rtc/api/webrtc",  // WebRTC negotiation
		"/rtc/api/mse",     // MediaSource Extensions
		"/rtc/api/streams", // Needed to query stream info
	}

	for _, sp := range safePaths {
		if strings.HasPrefix(path, sp) {
			allowed = true
			break
		}
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

func syncStreamToGo2RTC(name, streamUrl string, isDelete bool) error {
	apiUser := "http://localhost:1984/api/streams"
	method := http.MethodPut
	if isDelete {
		method = http.MethodDelete
	}

	reqUrl := fmt.Sprintf("%s?name=%s&src=%s", apiUser, url.QueryEscape(name), url.QueryEscape(streamUrl))
	if isDelete {
		reqUrl = fmt.Sprintf("%s?src=%s", apiUser, url.QueryEscape(name))
	}

	req, err := http.NewRequest(method, reqUrl, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("Sync stream %s response: %s (Status: %d)", name, string(body), resp.StatusCode)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("go2rtc api returned status %d: %s", resp.StatusCode, string(body))
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

func secureRTCProxyHandler(w http.ResponseWriter, r *http.Request) {
	// Security: Block access to the root of the RTC proxy to hide the dashboard
	// but allow stream-related files and necessary API endpoints.
	path := r.URL.Path
	if path == "/rtc/" || path == "/rtc" {
		http.Error(w, "Access Denied: The dashboard is restricted.", http.StatusForbidden)
		return
	}

	allowed := false

	// Allow all API sub-paths needed for streaming
	if strings.HasPrefix(path, "/rtc/api/") {
		allowed = true
	}

	// Allow stream.html player page
	if strings.HasPrefix(path, "/rtc/stream.html") {
		allowed = true
	}

	// Allow all static assets (.js, .css, .ico, .wasm, etc.) needed by the player
	// This avoids whack-a-mole every time go2rtc adds a new dependency
	staticExts := []string{".js", ".css", ".ico", ".png", ".svg", ".wasm", ".map"}
	for _, ext := range staticExts {
		if strings.HasSuffix(path, ext) {
			allowed = true
			break
		}
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
	cfgPath := "go2rtc.yaml"
	cfgMgr := config.NewConfigManager(cfgPath)

	log.Println("Initializing Stream Manager...")
	streamMgr := stream.NewManager(cfgMgr)

	// DB Setup
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "data/web-tr.db"
	}
	
	store, err := db.NewStore(dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	streamMgr.Store = store

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
					if err := syncStreamToGo2RTC(s.Name, s.URL, false); err != nil {
						log.Printf("Failed to sync stream %s: %v", s.Name, err)
					} else {
						log.Printf("Synced stream %s", s.Name)
					}
				}
			}
		}
	}()
	defer streamMgr.Stop()

	// Public Share Page
	http.HandleFunc("/share", func(w http.ResponseWriter, r *http.Request) {
		streamName := r.URL.Query().Get("stream")
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

	http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
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
				"Error":   errorMsg,
				"Success": successMsg,
				"Mode":    mode,
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
					Expiry:           expiry,
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
		if r.Method == http.MethodPost {
			username := r.FormValue("username")
			password := r.FormValue("password")
			fullName := r.FormValue("full_name")
			email := r.FormValue("email")
			whatsapp := r.FormValue("whatsapp")

			if username == "" || password == "" {
				http.Redirect(w, r, "/login?error=Username+and+password+are+required&mode=register", http.StatusSeeOther)
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
				SubscriptionPlan: "Free",
				FullName:         fullName,
				Email:            email,
				Whatsapp:         whatsapp,
			}

			if err := store.CreateUserFull(newUser, password); err != nil {
				log.Printf("Registration error: %v", err)
				http.Redirect(w, r, "/login?error=Failed+to+register+user&mode=register", http.StatusSeeOther)
				return
			}

			// Success, redirect to login
			http.Redirect(w, r, "/login?success=Registration+successful.+Please+log+in.", http.StatusSeeOther)
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

	http.HandleFunc("/api/sysinfo", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			stats := sysinfo.GetStats()
			stats.StreamCount = streamMgr.GetActiveCount()

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

		tmpl, err := template.ParseFiles("web/templates/landing.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		tmpl.Execute(w, nil)
	})

	http.HandleFunc("/docs/gateway", func(w http.ResponseWriter, r *http.Request) {
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
		tmpl.Execute(w, map[string]interface{}{
			"Streams": streams,
			"Session": sess,
		})
	})

	http.HandleFunc("/commandcenter", adminHandler)
	http.HandleFunc("/admin", adminHandler)

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
				Name    string  `json:"name"`
				URL     string  `json:"url"`
				Backend string  `json:"backend,omitempty"`
				Lat     float64 `json:"lat"`
				Lng     float64 `json:"lng"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			// Default to go2rtc if not specified
			if req.Backend == "" {
				req.Backend = "go2rtc"
			}

			// Enforce subscription quota limits for non-admins
			if sess.Role != "admin" {
				limit := 2 // default Free
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

			if err := store.AddStream(models.Stream{
				Name: req.Name,
				URL: req.URL,
				Backend: req.Backend,
				Lat: req.Lat,
				Lng: req.Lng,
				Enabled: true,
				UserID: sess.UserID,
				IsPublic: false,
			}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			
			streamMgr.SyncFromDB()

			streamMgr.SyncFromDB()

			// Route stream creation to the correct streaming engine
			if req.Backend == "go2rtc" || req.Backend == "ffmpeg" {
				urlToSync := req.URL
				if req.Backend == "ffmpeg" {
					urlToSync = "ffmpeg:" + req.URL + "#video=h264#hardware"
				}
				if err := syncStreamToGo2RTC(req.Name, urlToSync, false); err != nil {
					log.Printf("Failed to sync stream to go2rtc/ffmpeg: %v", err)
				}
			} else if req.Backend == "mediamtx" {
				if err := syncStreamToMediaMTX(req.Name, req.URL, false); err != nil {
					log.Printf("Failed to sync stream to MediaMTX: %v", err)
				}
			}

			w.WriteHeader(http.StatusCreated)
			return
		}

		if r.Method == http.MethodPut {
			var req struct {
				Name         string  `json:"name"`
				URL          string  `json:"url"`
				Backend      string  `json:"backend,omitempty"`
				OriginalName string  `json:"originalName"`
				Lat          float64 `json:"lat"`
				Lng          float64 `json:"lng"`
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

			// Use Manager Update
			streams, _ := streamMgr.GetStreams()
			isEnabled := true
			for _, s := range streams {
				if s.Name == req.OriginalName {
					isEnabled = s.Enabled
					break
				}
			}
			if err := streamMgr.UpdateStream(req.OriginalName, req.Name, req.URL, req.Lat, req.Lng, isEnabled); err != nil {
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
					syncStreamToGo2RTC(req.OriginalName, "", true)
				}
				if err := syncStreamToGo2RTC(req.Name, urlToSync, false); err != nil {
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
				if err := syncStreamToGo2RTC(stream.Name, "", true); err != nil {
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

			// Add stream (default to go2rtc for CSV imports)
			if err := streamMgr.AddStream(name, streamURL, "go2rtc", lat, lng, true); err != nil {
				failCount++
				errors = append(errors, fmt.Sprintf("Row %d (%s): %v", lineNum, name, err))
				continue
			}

			// Sync with Go2RTC
			if err := syncStreamToGo2RTC(name, streamURL, false); err != nil {
				log.Printf("Failed to sync stream %s to go2rtc: %v", name, err)
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
			line := fmt.Sprintf("\"%s\",\"%s\",%f,%f\n", s.Name, s.URL, s.Lat, s.Lng)
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
					syncStreamToGo2RTC(name, "", true)
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
				if req.Action == "disable" {
					syncStreamToGo2RTC(name, "", true) // Delete from active proxy memory
				} else {
					// Enable: Re-sync to Go2RTC
					streams, _ := streamMgr.GetStreams()
					for _, s := range streams {
						if s.Name == name {
							syncStreamToGo2RTC(s.Name, s.URL, false)
							break
						}
					}
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": successCount})
	}))

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

		resolution, err := streamMgr.ProbeStream(rawUrl)
		if err != nil {
			log.Printf("Probe failed: %v", err)
			http.Error(w, fmt.Sprintf("Probe failed: %v", err), http.StatusBadRequest)
			return
		}

		if req.Name != "" && resolution != "" && resolution != "Unknown" {
			log.Printf("Updating resolution for %s: %s", req.Name, resolution)
			store.UpdateStreamResolution(req.Name, resolution)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf("OK|%s", resolution)))
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
	http.HandleFunc("/api/stream.mp4", proxyToGo2RTC) // MSE/MP4

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

	timelapseMgr := timelapse.NewManager("timelapse.json")
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
			if u.EnableSupport {
				supportEnabledMap[u.ID] = true
			}
		}

		var filtered []models.Stream
		for _, s := range streams {
			if s.UserID == sess.UserID || supportEnabledMap[s.UserID] {
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
