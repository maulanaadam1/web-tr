package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
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
    if buyerName != "" { payload["buyerName"] = buyerName }
    if buyerEmail != "" { payload["buyerEmail"] = buyerEmail }

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
    req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("http error: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

    fmt.Println("Raw response:", string(respBody))

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

func main() {
    url, sess, err := createIPPayment(
        "0000005640445767", "SANDBOXEEFDFCF9-E4B2-4402-847B-4409D63CCE67", false,
        "Premium Plan", 35000, "R2G-X123",
        "Test Buyer", "test@example.com",
        "http://localhost/success", "http://localhost/cancel", "http://localhost/notify",
    )
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(url, sess)
}
