package models

import "time"

// Plan represents a subscription plan with pricing
type Plan struct {
	ID           int       `json:"id"`
	Name         string    `json:"name"`
	Label        string    `json:"label"`
	Price        int       `json:"price"`
	DurationDays int       `json:"duration_days"`
	MaxCameras   int       `json:"max_cameras"`
	IsActive     bool      `json:"is_active"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Order represents a payment transaction
type Order struct {
	ID          int       `json:"id"`
	ReferenceID string    `json:"reference_id"`
	UserID      int       `json:"user_id"`
	PlanName    string    `json:"plan_name"`
	Amount      int       `json:"amount"`
	Status      string    `json:"status"`
	PaymentURL  string    `json:"payment_url"`
	SessionID   string    `json:"session_id"`
	PaidAt      *time.Time `json:"paid_at,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}
