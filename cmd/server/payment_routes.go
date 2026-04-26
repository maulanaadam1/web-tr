package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"web-tr/internal/models"
)

// ─── iPaymu Helper ────────────────────────────────────────────────────────────

func createIPPayment(va, apiKey string, production bool,
	productName string, price int64, refID string,
	buyerName, buyerEmail string,
	returnURL, cancelURL, notifyURL string,
) (paymentURL, sessionID string, err error) {
	endpoint := "https://sandbox.ipaymu.com/api/v2/payment"
	if production {
		endpoint = "https://my.ipaymu.com/api/v2/payment"
	}

	body, _ := json.Marshal(map[string]interface{}{
		"product":     []string{productName},
		"qty":         []int{1},
		"price":       []int64{price},
		"returnUrl":   returnURL,
		"cancelUrl":   cancelURL,
		"notifyUrl":   notifyURL,
		"referenceId": refID,
		"buyerName":   buyerName,
		"buyerEmail":  buyerEmail,
	})

	// Generate signature: POST:<va>:<sha256(body)>:<apiKey>  --  HMAC-SHA256
	bodyHash := sha256.Sum256(body)
	bodyHashStr := hex.EncodeToString(bodyHash[:])
	stringToSign := "POST:" + va + ":" + strings.ToLower(bodyHashStr) + ":" + apiKey
	h := hmac.New(sha256.New, []byte(apiKey))
	h.Write([]byte(stringToSign))
	signature := hex.EncodeToString(h.Sum(nil))

	req, _ := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("va", va)
	req.Header.Set("signature", signature)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("http error: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Status  int    `json:"Status"`
		Message string `json:"Message"`
		Data    struct {
			SessionID string `json:"SessionId"`
			URL       string `json:"Url"`
		} `json:"Data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", fmt.Errorf("parse error: %w (body: %s)", err, string(respBody))
	}
	if result.Status != 200 {
		return "", "", fmt.Errorf("iPaymu %d: %s", result.Status, result.Message)
	}
	return result.Data.URL, result.Data.SessionID, nil
}

// ─── Payment Routes Registration ─────────────────────────────────────────────

func registerPaymentRoutes() {

	// Public: List active plans (for pricing page)
	http.HandleFunc("/api/plans", func(w http.ResponseWriter, r *http.Request) {
		plans, err := globalStore.GetAllPlans()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(plans)
	})

	// Admin: Get / Update plan pricing
	http.HandleFunc("/api/admin/plans", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		sess := r.Context().Value(sessionContextKey).(Session)
		if sess.Role != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodGet {
			plans, err := globalStore.GetAllPlans()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(plans)
			return
		}
		if r.Method == http.MethodPut {
			var req models.Plan
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "Invalid request", http.StatusBadRequest)
				return
			}
			if err := globalStore.UpdatePlan(req); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"message": "Plan updated"})
			return
		}
	}))

	// User: Create payment
	http.HandleFunc("/api/payment/create", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sess := r.Context().Value(sessionContextKey).(Session)

		var req struct {
			PlanName string `json:"plan"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		plan, err := globalStore.GetPlanByName(req.PlanName)
		if err != nil || plan == nil {
			http.Error(w, "Plan not found", http.StatusNotFound)
			return
		}
		if !plan.IsActive {
			http.Error(w, "Plan currently unavailable", http.StatusBadRequest)
			return
		}

		ipaymuVA := os.Getenv("IPAYMU_VA")
		ipaymuKey := os.Getenv("IPAYMU_API_KEY")
		production := os.Getenv("IPAYMU_PRODUCTION") == "true"
		appURL := os.Getenv("APP_URL")
		if appURL == "" {
			appURL = "https://localhost"
		}

		if ipaymuVA == "" || ipaymuKey == "" {
			http.Error(w, "Payment gateway not configured. Set IPAYMU_VA and IPAYMU_API_KEY.", http.StatusServiceUnavailable)
			return
		}

		// Unique reference ID
		refBytes := make([]byte, 6)
		rand.Read(refBytes)
		refID := fmt.Sprintf("R2G-%d-%s", sess.UserID, hex.EncodeToString(refBytes))

		user, _ := globalStore.GetUserByID(sess.UserID)
		buyerName, buyerEmail := sess.Username, ""
		if user != nil {
			if user.FullName != "" {
				buyerName = user.FullName
			}
			buyerEmail = user.Email
		}

		if err := globalStore.CreateOrder(refID, sess.UserID, plan.Name, plan.Price); err != nil {
			http.Error(w, "Failed to create order: "+err.Error(), http.StatusInternalServerError)
			return
		}

		payURL, sessionID, err := createIPPayment(
			ipaymuVA, ipaymuKey, production,
			plan.Label, int64(plan.Price), refID,
			buyerName, buyerEmail,
			appURL+"/payment/success",
			appURL+"/payment/cancel",
			appURL+"/api/payment/callback",
		)
		if err != nil {
			log.Printf("[Payment] iPaymu error: %v", err)
			http.Error(w, "Payment gateway error: "+err.Error(), http.StatusBadGateway)
			return
		}

		globalStore.UpdateOrderPaymentURL(refID, payURL, sessionID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"payment_url":  payURL,
			"reference_id": refID,
		})
	}))

	// iPaymu Callback (Webhook) — called by iPaymu after payment
	http.HandleFunc("/api/payment/callback", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		refID := r.FormValue("referenceId")
		status := r.FormValue("status") // "berhasil" | "pending" | "gagal"
		log.Printf("[Payment Callback] refID=%s status=%s", refID, status)

		if status == "berhasil" && refID != "" {
			order, err := globalStore.MarkOrderPaid(refID)
			if err != nil {
				log.Printf("[Callback] MarkOrderPaid: %v", err)
			} else if order != nil {
				plan, _ := globalStore.GetPlanByName(order.PlanName)
				if plan != nil {
					now := time.Now()
					user, _ := globalStore.GetUserByID(order.UserID)
					if user != nil {
						newExpiry := now.AddDate(0, 0, plan.DurationDays)
						if user.ExpiresAt.After(now) {
							newExpiry = user.ExpiresAt.AddDate(0, 0, plan.DurationDays)
						}
						user.SubscriptionPlan = plan.Name
						user.ExpiresAt = newExpiry
						globalStore.UpdateUserFull(*user)

						// Sync in-memory sessions
						sessionMutex.Lock()
						for token, s := range activeSessions {
							if s.UserID == order.UserID {
								s.SubscriptionPlan = plan.Name
								s.SubExpiry = newExpiry
								activeSessions[token] = s
							}
						}
						sessionMutex.Unlock()
						log.Printf("[Callback] User %d -> %s until %s", order.UserID, plan.Name, newExpiry.Format("2006-01-02"))
					}
				}
			}
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	// Redirect pages after payment
	http.HandleFunc("/payment/success", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin?payment=success", http.StatusSeeOther)
	}))
	http.HandleFunc("/payment/cancel", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin?payment=cancelled", http.StatusSeeOther)
	}))

	// Admin: Order history
	http.HandleFunc("/api/admin/orders", sessionAuth(func(w http.ResponseWriter, r *http.Request) {
		sess := r.Context().Value(sessionContextKey).(Session)
		if sess.Role != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		orders, err := globalStore.GetRecentOrders(100)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(orders)
	}))
}

