package models

import "time"

type License struct {
	ID           int        `json:"id"`
	Key          string     `json:"key"`
	Plan         string     `json:"plan"`
	DurationDays int        `json:"duration_days"`
	IsUsed       bool       `json:"is_used"`
	UsedByUserID int        `json:"used_by_user_id"`
	UsedAt       *time.Time `json:"used_at"`
	CreatedAt    time.Time  `json:"created_at"`
}
