package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"net/url"
	"crypto/rand"
	"time"
)

type Stream struct {
	Name        string  `json:"name"`
	DisplayName string  `json:"display_name"`
	URL         string  `json:"url"`
	Backend     string  `json:"backend"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
	Enabled     bool    `json:"enabled"`
}

func generateUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func main() {
	baseURL := "https://rtsp2go.campod.my.id"
	adminUser := "admin"
	adminPass := "admin123"

	// 0. Login to get cookie
	loginData := url.Values{}
	loginData.Set("username", adminUser)
	loginData.Set("password", adminPass)
	
	client := &http.Client{Timeout: 10 * time.Second}
	
	reqLogin, _ := http.NewRequest("POST", baseURL+"/login", strings.NewReader(loginData.Encode()))
	reqLogin.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respLogin, err := client.Do(reqLogin)
	if err != nil {
		fmt.Println("Login err:", err)
		return
	}
	defer respLogin.Body.Close()
	
	var authCookie *http.Cookie
	for _, cookie := range respLogin.Cookies() {
		if cookie.Name == "auth-session" {
			authCookie = cookie
			break
		}
	}
	
	if authCookie == nil {
		fmt.Println("Failed to get auth cookie!")
		return
	}

	// 1. Get Streams
	req, _ := http.NewRequest("GET", baseURL+"/api/streams", nil)
	req.AddCookie(authCookie)
	
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error fetching streams:", err)
		return
	}
	defer resp.Body.Close()

	var streams []Stream
	json.NewDecoder(resp.Body).Decode(&streams)

	fmt.Printf("Found %d streams. Checking for missing UUIDs...\n", len(streams))

	for _, s := range streams {
		if !strings.Contains(s.Name, "-") || len(s.Name) < 32 { // Basic UUID check
			fmt.Printf("Migrating stream: %s\n", s.Name)
			
			// Generate UUID target
			newUUID := generateUUID()

			// 2. DELETE old stream
			delURL := fmt.Sprintf("%s/api/streams?name=%s", baseURL, url.QueryEscape(s.Name))
			delReq, _ := http.NewRequest("DELETE", delURL, nil)
			delReq.AddCookie(authCookie)
			resp, err = client.Do(delReq)
			if err != nil {
				fmt.Println("  Failed to delete:", err)
				continue
			}
			resp.Body.Close()

			// 3. POST new stream mapped to UUID
			s.DisplayName = s.Name
			s.Name = newUUID

			jsonData, _ := json.Marshal(s)
			postReq, _ := http.NewRequest("POST", baseURL+"/api/streams", bytes.NewBuffer(jsonData))
			postReq.Header.Set("Content-Type", "application/json")
			postReq.AddCookie(authCookie)
			resp, err = client.Do(postReq)
			if err != nil {
				fmt.Println("  Failed to recreate:", err)
				continue
			}
			body, _ := ioutil.ReadAll(resp.Body)
			resp.Body.Close()

			if resp.StatusCode == 200 || resp.StatusCode == 201 {
				fmt.Printf("  Success! %s -> %s\n", s.DisplayName, s.Name)
			} else {
				fmt.Printf("  Failed! Status: %d, Res: %s\n", resp.StatusCode, string(body))
			}
		} else {
			fmt.Printf("Stream %s already has UUID format.\n", s.Name)
		}
	}
	fmt.Println("Migration complete!")
}
