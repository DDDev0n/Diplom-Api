package processing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"bank-processing-data-subsystem/internal/store"
)

func TestClientLogsInAndProcessesPayment(t *testing.T) {
	t.Parallel()

	var loginCalls int
	var processCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			loginCalls++
			if r.Method != http.MethodPost {
				t.Fatalf("login method = %s, want POST", r.Method)
			}
			var req loginRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req.Username != "tester" || req.Role != "USER" {
				t.Fatalf("login request = %+v", req)
			}
			_ = json.NewEncoder(w).Encode(loginResponse{Token: "external-token"})
		case "/api/v1/payments/process":
			processCalls++
			if got := r.Header.Get("Authorization"); got != "Bearer external-token" {
				t.Fatalf("Authorization = %q, want Bearer external-token", got)
			}
			var req processingRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req.Amount != "123.45" {
				t.Fatalf("amount = %q, want 123.45", req.Amount)
			}
			if req.PaymentType != "TRANSFER" {
				t.Fatalf("paymentType = %q, want TRANSFER", req.PaymentType)
			}
			_ = json.NewEncoder(w).Encode(Result{Status: "COMPLETED", FraudScore: 7})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL})
	result, err := client.Score(context.Background(), store.Payment{
		ID:          42,
		SenderID:    1,
		RecipientID: 2,
		Amount:      12345,
		PaymentType: "SINGLE",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != store.StatusCompleted || result.FraudScore != 7 {
		t.Fatalf("result = %+v", result)
	}
	if loginCalls != 1 || processCalls != 1 {
		t.Fatalf("loginCalls = %d, processCalls = %d", loginCalls, processCalls)
	}
}

func TestClientUsesConfiguredBearerTokenWithoutLogin(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" {
			t.Fatal("login endpoint should not be called when PROCESSING_AUTH_TOKEN is set")
		}
		if got := r.Header.Get("Authorization"); got != "Bearer configured-token" {
			t.Fatalf("Authorization = %q, want Bearer configured-token", got)
		}
		_ = json.NewEncoder(w).Encode(Result{Status: "COMPLETED"})
	}))
	defer server.Close()

	client := NewClient(Config{
		BaseURL:   server.URL,
		AuthToken: "Bearer configured-token",
	})
	if _, err := client.Score(context.Background(), store.Payment{ID: 1, SenderID: 1, RecipientID: 2, Amount: 100}); err != nil {
		t.Fatal(err)
	}
}

func TestClientMapsProcessingRateLimitToRejectedResult(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient(Config{
		BaseURL:   server.URL,
		AuthToken: "configured-token",
	})
	result, err := client.Score(context.Background(), store.Payment{ID: 1, SenderID: 1, RecipientID: 2, Amount: 100})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != store.StatusRejected {
		t.Fatalf("status = %q, want %q", result.Status, store.StatusRejected)
	}
	if result.Reason == "" {
		t.Fatal("reason is empty")
	}
}
