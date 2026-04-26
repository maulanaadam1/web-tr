package models

import "time"

type Node struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`      // Nama server (misal: "Node-JKT-01")
	URL       string    `json:"url"`       // Alamat IP atau Domain (misal: "http://103.x.x.x:1984")
	RtspPort  int       `json:"rtsp_port"` // Port RTSP push (default: 8554)
	Secret    string    `json:"secret"`    // Token keamanan untuk komunikasi antar server
	IsActive  bool      `json:"is_active"`
	Location  string    `json:"location"`
	CreatedAt time.Time `json:"created_at"`
	
	// Metrics (Volatile)
	CPUUsage float64 `json:"cpu_usage"`
	RAMUsage float64 `json:"ram_usage"`
	StreamCount int  `json:"stream_count"`
}
