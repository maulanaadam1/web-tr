package payment

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	SandboxURL    = "https://sandbox.ipaymu.com/api/v2/payment"
	ProductionURL = "https://my.ipaymu.com/api/v2/payment"
)

// IPaymuClient represents an iPaymu API client
type IPaymuClient struct {
	VA          string
	APIKey      string
	ProductionMode bool
}

// PaymentRequest represents the request to create a payment
type PaymentRequest struct {
	Product     []string  `json:"product"`
	Qty         []int     `json:"qty"`
	Price       []int64   `json:"price"`
	ReturnURL   string    `json:"returnUrl"`
	CancelURL   string    `json:"cancelUrl"`
	NotifyURL   string    `json:"notifyUrl"`
	ReferenceID string    `json:"referenceId"`
	BuyerName   string    `json:"buyerName,omitempty"`
	BuyerEmail  string    `json:"buyerEmail,omitempty"`
	BuyerPhone  string    `json:"buyerPhone,omitempty"`
}

// PaymentResponse represents the iPaymu API response
type PaymentResponse struct {
	Status  int    `json:"Status"`
	Message string `json:"Message"`
	Data    struct {
		SessionID   string `json:"SessionId"`
		TransactionID string `json:"TransactionId"`
		PaymentURL  string `json:"Url"`
	} `json:"Data"`
}

// NewClient creates a new iPaymu client
func NewClient(va, apiKey string, production bool) *IPaymuClient {
	return &IPaymuClient{
		VA:             va,
		APIKey:         apiKey,
		ProductionMode: production,
	}
}

// generateSignature generates the HMAC-SHA256 signature for iPaymu
func (c *IPaymuClient) generateSignature(body []byte) string {
	bodyHash := sha256.Sum256(body)
	bodyHashStr := hex.EncodeToString(bodyHash[:])
	stringToSign := "POST:" + c.VA + ":" + strings.ToLower(bodyHashStr) + ":" + c.APIKey

	h := hmac.New(sha256.New, []byte(c.APIKey))
	h.Write([]byte(stringToSign))
	return hex.EncodeToString(h.Sum(nil))
}

// CreatePayment creates a new payment request and returns the payment URL
func (c *IPaymuClient) CreatePayment(req PaymentRequest) (*PaymentResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	signature := c.generateSignature(body)

	apiURL := SandboxURL
	if c.ProductionMode {
		apiURL = ProductionURL
	}

	httpReq, err := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("va", c.VA)
	httpReq.Header.Set("signature", signature)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var payResp PaymentResponse
	if err := json.Unmarshal(respBody, &payResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w (body: %s)", err, string(respBody))
	}

	if payResp.Status != 200 {
		return nil, fmt.Errorf("iPaymu error %d: %s", payResp.Status, payResp.Message)
	}

	return &payResp, nil
}
