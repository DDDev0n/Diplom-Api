package processing

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"bank-processing-data-subsystem/internal/store"
)

type Config struct {
	BaseURL      string
	LoginPath    string
	ProcessPath  string
	AuthToken    string
	AuthUsername string
	AuthRole     string
	TLSConfig    *tls.Config
}

type Result struct {
	Status     string `json:"status"`
	FraudScore int    `json:"fraudScore"`
	Reason     string `json:"message,omitempty"`
}

type Client struct {
	baseURL      string
	loginPath    string
	processPath  string
	authToken    string
	authUsername string
	authRole     string
	httpClient   *http.Client
	mu           sync.Mutex
	cachedToken  string
}

func NewClient(cfg Config) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = cfg.TLSConfig
	loginPath := cfg.LoginPath
	if loginPath == "" {
		loginPath = "/api/v1/auth/login"
	}
	processPath := cfg.ProcessPath
	if processPath == "" {
		processPath = "/api/v1/payments/process"
	}
	username := cfg.AuthUsername
	if username == "" {
		username = "tester"
	}
	role := cfg.AuthRole
	if role == "" {
		role = "USER"
	}
	return &Client{
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		loginPath:    "/" + strings.TrimLeft(loginPath, "/"),
		processPath:  "/" + strings.TrimLeft(processPath, "/"),
		authToken:    strings.TrimSpace(strings.TrimPrefix(cfg.AuthToken, "Bearer ")),
		authUsername: username,
		authRole:     role,
		httpClient:   &http.Client{Timeout: 5 * time.Second, Transport: transport},
	}
}

func (c *Client) Score(ctx context.Context, payment store.Payment) (Result, error) {
	body, _ := json.Marshal(processingRequest{
		SenderID:       idToUUID(payment.SenderID),
		RecipientID:    idToUUID(payment.RecipientID),
		Amount:         centsToDecimal(payment.Amount),
		Currency:       "RUB",
		Description:    payment.Description,
		PaymentType:    mapPaymentType(payment.PaymentType),
		IdempotencyKey: fmt.Sprintf("local-payment-%d", payment.ID),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+c.processPath, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	token, err := c.token(ctx)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("processing status %d", resp.StatusCode)
	}

	var result Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Result{}, err
	}
	if result.Status == "" {
		return Result{}, fmt.Errorf("processing response does not contain status")
	}
	result.Status = mapProcessingStatus(result.Status)
	return result, nil
}

func (c *Client) token(ctx context.Context) (string, error) {
	if c.authToken != "" {
		return c.authToken, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cachedToken != "" {
		return c.cachedToken, nil
	}

	body, _ := json.Marshal(loginRequest{
		Username: c.authUsername,
		Role:     c.authRole,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+c.loginPath, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("processing auth status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var out loginResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Token == "" {
		return "", fmt.Errorf("processing auth response does not contain token")
	}
	c.cachedToken = out.Token
	return c.cachedToken, nil
}

type processingRequest struct {
	SenderID       string `json:"senderId"`
	RecipientID    string `json:"recipientId"`
	Amount         string `json:"amount"`
	Currency       string `json:"currency"`
	Description    string `json:"description,omitempty"`
	PaymentType    string `json:"paymentType"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type loginRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

type loginResponse struct {
	Token string `json:"token"`
}

func idToUUID(id int64) string {
	unsigned := uint64(id)
	return fmt.Sprintf("%08x-%04x-%04x-0000-000000000000", uint32(unsigned>>32), uint16(unsigned>>16), uint16(unsigned))
}

func centsToDecimal(cents int64) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

func mapPaymentType(paymentType string) string {
	switch strings.ToUpper(paymentType) {
	case "MASS_PAYOUT", "PAYMENT":
		return "PAYMENT"
	case "WITHDRAWAL":
		return "WITHDRAWAL"
	default:
		return "TRANSFER"
	}
}

func mapProcessingStatus(status string) string {
	switch strings.ToUpper(status) {
	case "COMPLETED":
		return store.StatusCompleted
	case "APPROVED":
		return store.StatusApproved
	case "REJECTED", "FAILED":
		return store.StatusRejected
	default:
		return store.StatusPending
	}
}
