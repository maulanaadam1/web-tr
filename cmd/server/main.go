package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"web-tr/internal/config"
	"web-tr/internal/db"
	"web-tr/internal/stream"
)

func proxyToGo2RTC(w http.ResponseWriter, r *http.Request) {
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

func main() {
	// Setup
	cfgPath := "go2rtc.yaml"
	cfgMgr := config.NewConfigManager(cfgPath)

	log.Println("Initializing Stream Manager...")
	streamMgr := stream.NewManager(cfgMgr)

	// DB Setup
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		log.Println("Connecting to Database...")
		store, err := db.NewStore(dbURL)
		if err != nil {
			log.Fatalf("Failed to connect to DB: %v", err)
		}
		streamMgr.Store = store
		log.Println("Database/PostgreSQL mode enabled")
	} else {
		log.Println("No DATABASE_URL found. Running in File/YAML mode.")
	}

	// Ensure config exists
	if err := streamMgr.EnsureConfig(); err != nil {
		log.Fatalf("Failed to ensure config: %v", err)
	}

	// Start Engine
	go func() {
		log.Println("Starting go2rtc...")
		if err := streamMgr.Start(); err != nil {
			log.Printf("Error starting go2rtc: %v", err)
			log.Println("Ensure go2rtc (or .exe) is in the current directory or PATH.")
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
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("web/templates/index.html", "web/templates/player.html") // Preload templates? No, separate logic
		// Just parse index for now
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}

		tmpl, err = template.ParseFiles("web/templates/index.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		streams, err := streamMgr.GetStreams()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("Rendering index with %d streams", len(streams))
		tmpl.Execute(w, map[string]interface{}{
			"Streams": streams,
		})
	})

	http.HandleFunc("/api/streams", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			streams, err := streamMgr.GetStreams()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(streams)
			return
		}

		if r.Method == http.MethodPost {
			var req struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if err := streamMgr.AddStream(req.Name, req.URL); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// Sync with Go2RTC
			if err := syncStreamToGo2RTC(req.Name, req.URL, false); err != nil {
				log.Printf("Failed to sync stream to go2rtc: %v", err)
			}

			w.WriteHeader(http.StatusCreated)
			return
		}

		if r.Method == http.MethodPut {
			var req struct {
				Name         string `json:"name"`
				URL          string `json:"url"`
				OriginalName string `json:"originalName"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			// Use Manager Update
			if err := streamMgr.UpdateStream(req.OriginalName, req.Name, req.URL); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// Handle Sync Logic
			// If name changed, delete old
			if req.OriginalName != "" && req.OriginalName != req.Name {
				syncStreamToGo2RTC(req.OriginalName, "", true)
			}
			// Add/Update new
			if err := syncStreamToGo2RTC(req.Name, req.URL, false); err != nil {
				log.Printf("Failed to sync stream to go2rtc: %v", err)
			}

			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == http.MethodDelete {
			name := r.URL.Query().Get("name")
			if name == "" {
				http.Error(w, "name is required", http.StatusBadRequest)
				return
			}
			if err := streamMgr.RemoveStream(name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// Sync with Go2RTC
			if err := syncStreamToGo2RTC(name, "", true); err != nil {
				log.Printf("Failed to remove stream from go2rtc: %v", err)
			}

			w.WriteHeader(http.StatusOK)
			return
		}
	})

	http.HandleFunc("/api/probe", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Basic cleanup if user pasted internal format
		rawUrl := req.URL
		// Strip ffmpeg/exec prefixes for probing the source directly if possible.
		// NOTE: if it is a complex ffmpeg command with filters, ffprobe might fail if treated as URL.
		// For MVP, we probe the raw URL if it looks like a URL.
		// Simple heuristic: if it starts with rtsp/http/tcp/udp

		log.Printf("Probing stream: %s", rawUrl)
		if err := streamMgr.ProbeStream(rawUrl); err != nil {
			log.Printf("Probe failed: %v", err)
			http.Error(w, fmt.Sprintf("Probe failed: %v", err), http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	http.HandleFunc("/api/discover", func(w http.ResponseWriter, r *http.Request) {
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

	})

	http.HandleFunc("/api/snapshot", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}

		// Look up stream URL
		streams, err := streamMgr.GetStreams()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var streamUrl string
		for _, s := range streams {
			if s.Name == name {
				streamUrl = s.URL
				break
			}
		}

		if streamUrl == "" {
			http.Error(w, "stream not found", http.StatusNotFound)
			return
		}

		// Prepare FFmpeg command
		// ffmpeg -y -rtsp_transport tcp -i <url> -vframes 1 -f image2pipe -vcodec mjpeg -
		// Strip ffmpeg: prefix if present for clean URL
		cleanUrl := streamUrl
		if len(cleanUrl) > 7 && cleanUrl[:7] == "ffmpeg:" {
			cleanUrl = cleanUrl[7:]
			// simplistic strip, might need more if complex args
			// But for snapshot, we usually want the raw source
		}

		args := []string{
			"-y",
			"-rtsp_transport", "tcp",
			"-i", cleanUrl,
			"-vframes", "1",
			"-f", "image2pipe",
			"-vcodec", "mjpeg",
			"-",
		}

		cmd := exec.Command("ffmpeg", args...)

		// Pipe output directly to response
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s.jpg\"", name))

		cmd.Stdout = w
		cmd.Stderr = os.Stderr // Log errors to server console

		if err := cmd.Run(); err != nil {
			log.Printf("Snapshot error for %s: %v", name, err)
			return
		}
	})

	// HLS & MSE Proxy Handlers
	http.HandleFunc("/api/stream.mp4", proxyToGo2RTC) // MSE/MP4

	// Start Server
	port := "8080"
	log.Printf("Server listening on http://localhost:%s", port)

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

	go func() {
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			log.Fatal(err)
		}
	}()

	<-stop
	log.Println("Shutting down...")
	streamMgr.Stop()
}
