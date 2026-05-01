//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

type authResponse struct {
	Token string `json:"token"`
	User  user   `json:"user"`
}

type user struct {
	ID int64 `json:"id"`
}

type payment struct {
	ID          int64  `json:"id"`
	Status      string `json:"status"`
	FraudScore  int    `json:"fraud_score"`
	ProcessedAt string `json:"processed_at"`
}

func TestAPIHealthSwaggerAndMetrics(t *testing.T) {
	api := env("API_BASE_URL", "http://localhost:8000")
	client := testClient()

	waitForAPI(t, client, api)

	assertStatus(t, client, http.MethodGet, api+"/health", nil, http.StatusOK, `"status":"ok"`)
	assertStatus(t, client, http.MethodGet, api+"/swagger", nil, http.StatusOK, "SwaggerUIBundle")
	assertStatus(t, client, http.MethodGet, api+"/openapi.json", nil, http.StatusOK, "Bank Processing Data Subsystem API")
	assertStatus(t, client, http.MethodGet, api+"/metrics", nil, http.StatusOK, "bank_api_uptime_seconds")
}

func TestMonitoringStack(t *testing.T) {
	client := testClient()
	prometheus := strings.TrimRight(env("PROMETHEUS_BASE_URL", "http://localhost:9090"), "/")
	grafana := strings.TrimRight(env("GRAFANA_BASE_URL", "http://localhost:3001"), "/")
	postgresExporter := strings.TrimRight(env("POSTGRES_EXPORTER_BASE_URL", "http://localhost:9187"), "/")
	nodeExporter := strings.TrimRight(env("NODE_EXPORTER_BASE_URL", "http://localhost:9100"), "/")
	cadvisor := strings.TrimRight(env("CADVISOR_BASE_URL", "http://localhost:8081"), "/")

	assertStatus(t, client, http.MethodGet, prometheus+"/-/ready", nil, http.StatusOK, "Prometheus Server is Ready")
	assertStatus(t, client, http.MethodGet, grafana+"/api/health", nil, http.StatusOK, `"database": "ok"`)
	assertStatus(t, client, http.MethodGet, postgresExporter+"/metrics", nil, http.StatusOK, "pg_up")
	assertStatus(t, client, http.MethodGet, nodeExporter+"/metrics", nil, http.StatusOK, "node_cpu_seconds_total")
	assertStatus(t, client, http.MethodGet, cadvisor+"/metrics", nil, http.StatusOK, "container_cpu_usage_seconds_total")
}

func TestPaymentFlowThroughAPIAndQueue(t *testing.T) {
	api := env("API_BASE_URL", "http://localhost:8000")
	client := testClient()

	waitForAPI(t, client, api)

	suffix := time.Now().UnixNano()
	recipient := register(t, client, api, map[string]any{
		"email":     fmt.Sprintf("recipient-%d@test.local", suffix),
		"password":  "123456",
		"full_name": "Integration Recipient",
		"role":      "CLIENT",
		"balance":   0,
	})
	sender := register(t, client, api, map[string]any{
		"email":     fmt.Sprintf("sender-%d@test.local", suffix),
		"password":  "123456",
		"full_name": "Integration Sender",
		"role":      "CLIENT",
		"balance":   50000000,
	})

	created := postPayment(t, client, api, sender.Token, map[string]any{
		"recipient_id": recipient.User.ID,
		"amount":       12345,
		"description":  "integration test payment",
		"payment_type": "SINGLE",
	})
	if created.ID == 0 {
		t.Fatal("created payment id is empty")
	}
	if created.Status != "PENDING" {
		t.Fatalf("new payment status = %q, want PENDING", created.Status)
	}

	got := getPayment(t, client, api, sender.Token, created.ID)
	if got.ID != created.ID {
		t.Fatalf("payment id = %d, want %d", got.ID, created.ID)
	}

	assertStatus(t, client, http.MethodGet, api+"/api/payments", bearer(sender.Token), http.StatusOK, fmt.Sprintf(`"id":%d`, created.ID))

	if strictExternalProcessing() {
		processed := waitForProcessedPayment(t, client, api, sender.Token, created.ID)
		if processed.Status == "PENDING" || processed.ProcessedAt == "" {
			t.Fatalf("payment was not processed by worker: %+v", processed)
		}
	}
}

func TestExternalProcessingReachable(t *testing.T) {
	base := strings.TrimRight(env("PROCESSING_BASE_URL", "https://pay.projectl.ru"), "/")
	healthPath := "/" + strings.TrimLeft(env("PROCESSING_HEALTH_PATH", "/actuator/health"), "/")
	client := testClient()

	req, err := http.NewRequest(http.MethodGet, base+healthPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		if strictExternalProcessing() {
			t.Fatalf("external processing is not reachable: %v", err)
		}
		t.Skipf("external processing is not reachable from this machine: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 && strictExternalProcessing() {
		t.Fatalf("external processing returned %s", resp.Status)
	}
	t.Logf("external processing responded with %s", resp.Status)
}

func testClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if truthy(os.Getenv("PROCESSING_INSECURE_SKIP_VERIFY")) {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // Integration test flag for a known external TLS mismatch.
	}
	return &http.Client{Timeout: 5 * time.Second, Transport: transport}
}

func waitForAPI(t *testing.T, client *http.Client, api string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, api+"/health", nil)
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		if ctx.Err() != nil {
			t.Fatalf("api did not become healthy: %v", ctx.Err())
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func register(t *testing.T, client *http.Client, api string, payload map[string]any) authResponse {
	t.Helper()
	var out authResponse
	doJSON(t, client, http.MethodPost, api+"/api/auth/register", nil, payload, http.StatusCreated, &out)
	if out.Token == "" || out.User.ID == 0 {
		t.Fatalf("register response is incomplete: %+v", out)
	}
	return out
}

func postPayment(t *testing.T, client *http.Client, api string, token string, payload map[string]any) payment {
	t.Helper()
	var out payment
	doJSON(t, client, http.MethodPost, api+"/api/payments", bearer(token), payload, http.StatusCreated, &out)
	return out
}

func getPayment(t *testing.T, client *http.Client, api string, token string, id int64) payment {
	t.Helper()
	var out payment
	doJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/payments/%d", api, id), bearer(token), nil, http.StatusOK, &out)
	return out
}

func waitForProcessedPayment(t *testing.T, client *http.Client, api string, token string, id int64) payment {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last payment
	for time.Now().Before(deadline) {
		last = getPayment(t, client, api, token, id)
		if last.Status != "PENDING" || last.ProcessedAt != "" {
			return last
		}
		time.Sleep(time.Second)
	}
	return last
}

func doJSON(t *testing.T, client *http.Client, method string, url string, headers http.Header, payload any, wantStatus int, out any) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s returned %d, want %d: %s", method, url, resp.StatusCode, wantStatus, string(raw))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("decode response: %v: %s", err, string(raw))
		}
	}
}

func assertStatus(t *testing.T, client *http.Client, method string, url string, headers http.Header, wantStatus int, wantBodyPart string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s returned %d, want %d: %s", method, url, resp.StatusCode, wantStatus, string(raw))
	}
	if wantBodyPart != "" && !strings.Contains(string(raw), wantBodyPart) {
		t.Fatalf("%s %s response does not contain %q: %s", method, url, wantBodyPart, string(raw))
	}
}

func bearer(token string) http.Header {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+token)
	return headers
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func strictExternalProcessing() bool {
	return truthy(os.Getenv("REQUIRE_EXTERNAL_PROCESSING"))
}

func truthy(value string) bool {
	return strings.EqualFold(value, "true") || value == "1" || strings.EqualFold(value, "yes")
}
