package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

func createIPPayment(va, apiKey string, production bool,
	productName string, price int64, refID string,
	buyerName, buyerEmail string,
	returnURL, cancelURL, notifyURL string,
) (paymentURL, sessionID string, err error) {
	endpoint := "https://sandbox.ipaymu.com/api/v2/payment"
	if production {
		endpoint = "https://my.ipaymu.com/api/v2/payment"
	}

	payload := map[string]interface{}{
		"product":     []string{productName},
		"qty":         []int{1},
		"price":       []int64{price},
		"returnUrl":   returnURL,
		"cancelUrl":   cancelURL,
		"notifyUrl":   notifyURL,
		"referenceId": refID,
	}
	if buyerName != "" {
		payload["buyerName"] = buyerName
	}
	if buyerEmail != "" {
		payload["buyerEmail"] = buyerEmail
	}

	body, _ := json.Marshal(payload)

	bodyHash := sha256.Sum256(body)
	bodyHashStr := hex.EncodeToString(bodyHash[:])
	stringToSign := "POST:" + va + ":" + strings.ToLower(bodyHashStr) + ":" + apiKey
	h := hmac.New(sha256.New, []byte(apiKey))
	h.Write([]byte(stringToSign))
	signature := hex.EncodeToString(h.Sum(nil))

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("va", va)
	req.Header.Set("signature", signature)

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

func registerPaymentRoutes() {
	http.HandleFunc("/payment/success", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("web/templates/payment_success.html")
		if err != nil {
			http.Error(w, "Payment Successful. Return to Dashboard.", http.StatusOK)
			return
		}
		tmpl.Execute(w, nil)
	})

	http.HandleFunc("/payment/cancel", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("web/templates/payment_cancel.html")
		if err != nil {
			http.Error(w, "Payment Cancelled. Return to Dashboard.", http.StatusOK)
			return
		}
		tmpl.Execute(w, nil)
	})

	http.HandleFunc("/api/payment/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		trxID := r.FormValue("trx_id")
		status := r.FormValue("status")
		statusCode := r.FormValue("status_code")
		refID := r.FormValue("reference_id")

		log.Printf("[Payment Callback] Ref: %s, TrxID: %s, Status: %s, Code: %s", refID, trxID, status, statusCode)

		if status == "berhasil" || statusCode == "1" {
			order, err := globalStore.GetOrderByRef(refID)
			if err != nil || order == nil {
				log.Printf("[Payment Callback] Error: order not found for Ref %s", refID)
				w.Write([]byte("OK"))
				return
			}

			if order.Status == "paid" {
				w.Write([]byte("OK"))
				return
			}

			user, err := globalStore.GetUserByID(order.UserID)
			if err != nil || user == nil {
				log.Printf("[Payment Callback] Error: user not found for ID %d", order.UserID)
				w.Write([]byte("OK"))
				return
			}

			_, err = globalStore.MarkOrderPaid(refID)
			if err != nil {
				log.Printf("[Payment Callback] Error updating order status: %v", err)
			}

			newExpiry := time.Now().AddDate(0, 1, 0)
			if user.ExpiresAt.After(time.Now()) {
				newExpiry = user.ExpiresAt.AddDate(0, 1, 0)
			}

			user.SubscriptionPlan = order.PlanName
			user.ExpiresAt = newExpiry
			err = globalStore.UpdateUserFull(*user)
			if err != nil {
				log.Printf("[Payment Callback] Error upgrading user plan: %v", err)
			} else {
				log.Printf("[Payment Callback] User %s upgraded to %s until %s", user.Username, order.PlanName, newExpiry)
			}
		}

		w.Write([]byte("OK"))
	})
}
