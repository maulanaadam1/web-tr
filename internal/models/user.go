package models

import "time"

type User struct {
	ID                     int       `json:"id"`
	Username               string    `json:"username"`
	PasswordHash           string    `json:"-"` // Don't expose password hash in JSON
	Salt                   string    `json:"-"`
	Role                   string    `json:"role"`
	FullName               string    `json:"full_name"`
	Email                  string    `json:"email"`
	Whatsapp               string    `json:"whatsapp"`
	IsActive               bool      `json:"is_active"`
	BroadcastNotifications bool      `json:"broadcast_notifications"`
	NotificationPaid       bool      `json:"notification_paid"`
	SubscriptionPlan       string    `json:"subscription_plan"`
	EnableSupport          bool      `json:"enable_support"`
	PublicToken            string    `json:"public_token"`
	DedicatedNodeID        int       `json:"dedicated_node_id"`
	ExpiresAt              time.Time `json:"expires_at"`
	CreatedAt              time.Time `json:"created_at"`
}
