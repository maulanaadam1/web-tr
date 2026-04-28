package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Config structs matched with the main gateway
type CameraConfig struct {
	Name      string `json:"name"`
	LocalRTSP string `json:"local_rtsp"`
}

type Config struct {
	ServerURL   string         `json:"server_url"`
	ApiUsername string         `json:"api_username"`
	ApiPassword string         `json:"api_password"`
	Cameras     []CameraConfig `json:"cameras"`
}

var config Config

func main() {
	fmt.Println("==========================================")
	fmt.Println("      RTSP2go GATEWAY CLI (LINUX)         ")
	fmt.Println("==========================================")

	// 1. Load config.json
	data, err := ioutil.ReadFile("config.json")
	if err != nil {
		fmt.Printf("❌ ERROR: File 'config.json' tidak ditemukan!\n")
		fmt.Println("Pastikan file config.json ada di folder yang sama dengan aplikasi ini.")
		os.Exit(1)
	}

	err = json.Unmarshal(data, &config)
	if err != nil {
		fmt.Printf("❌ ERROR: Format config.json salah: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Config Loaded: %d Cameras detected.\n", len(config.Cameras))
	fmt.Printf("🔗 Central Server: %s\n", config.ServerURL)
	fmt.Println("------------------------------------------")

	// 2. Start all streams
	for _, cam := range config.Cameras {
		go runCameraWorker(cam)
	}

	// 3. Wait for Exit Signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	
	fmt.Println("🚀 Gateway is running in background mode.")
	fmt.Println("Tekan CTRL+C untuk berhenti.")
	
	<-stop
	fmt.Println("\n⚠️  Gatewat shutting down...")
}

func runCameraWorker(cam CameraConfig) {
	for {
		fmt.Printf("[%s] 🔄 Registering stream to server...\n", cam.Name)
		
		// Register via API
		success := registerToCentral(cam)
		if !success {
			fmt.Printf("[%s] ❌ Registration failed. Retrying in 10s...\n", cam.Name)
			time.Sleep(10 * time.Second)
			continue
		}

		fmt.Printf("[%s] 🟢 Registered! Pushing stream via FFmpeg...\n", cam.Name)

		// Exec FFmpeg to push
		// We use -re for original rate and -c copy to save CPU
		serverURL := strings.TrimSuffix(config.ServerURL, "/")
		pushURL := fmt.Sprintf("%s/push/%s", serverURL, cam.Name)
		
		cmd := exec.Command("ffmpeg", 
			"-rtsp_transport", "tcp",
			"-i", cam.LocalRTSP,
			"-c", "copy",
			"-f", "rtsp", 
			pushURL,
		)

		// Capture error output for logs
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		err := cmd.Run()
		
		fmt.Printf("[%s] 🔴 Stream stopped: %v\n", cam.Name, err)
		if stderr.Len() > 0 {
			fmt.Printf("[%s] FFmpeg Log: %s\n", cam.Name, stderr.String())
		}
		
		fmt.Printf("[%s] ⏳ Restarting in 5s...\n", cam.Name)
		time.Sleep(5 * time.Second)
	}
}

func registerToCentral(cam CameraConfig) bool {
	serverURL := strings.TrimSuffix(config.ServerURL, "/")
	apiURL := fmt.Sprintf("%s/api/streams/register", serverURL)

	payload, _ := json.Marshal(map[string]string{
		"name": cam.Name,
		"mode": "bridge", // Use bridge for direct push
	})

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(payload))
	if err != nil {
		return false
	}

	req.SetBasicAuth(config.ApiUsername, config.ApiPassword)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated
}
