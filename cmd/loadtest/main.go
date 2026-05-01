package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
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
	ProcessedAt string `json:"processed_at"`
}

type result struct {
	name       string
	statusCode int
	duration   time.Duration
	err        error
}

func main() {
	apiBase := flag.String("api", env("LOAD_API_BASE_URL", "http://localhost:8000"), "API base URL")
	users := flag.Int("users", envInt("LOAD_USERS", 20), "number of sender/recipient user pairs")
	payments := flag.Int("payments", envInt("LOAD_PAYMENTS", 200), "number of payments to create")
	concurrency := flag.Int("concurrency", envInt("LOAD_CONCURRENCY", 10), "parallel payment creation workers")
	amount := flag.Int64("amount", envInt64("LOAD_AMOUNT", 1000), "payment amount in cents")
	waitProcessing := flag.Bool("wait-processing", envBool("LOAD_WAIT_PROCESSING", false), "wait until created payments leave PENDING")
	timeout := flag.Duration("timeout", envDuration("LOAD_TIMEOUT", 2*time.Minute), "overall load test timeout")
	flag.Parse()

	if *users <= 0 || *payments <= 0 || *concurrency <= 0 {
		log.Fatal("users, payments and concurrency must be positive")
	}

	base := strings.TrimRight(*apiBase, "/")
	client := &http.Client{Timeout: 10 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	if err := waitForAPI(ctx, client, base); err != nil {
		log.Fatalf("api is not ready: %v", err)
	}

	runID := time.Now().UnixNano()
	fmt.Printf("load test: api=%s users=%d payments=%d concurrency=%d wait_processing=%t\n", base, *users, *payments, *concurrency, *waitProcessing)

	pairs, registerResults, err := createUserPairs(ctx, client, base, runID, *users)
	if err != nil {
		log.Fatal(err)
	}
	printSummary("register", registerResults)

	paymentResults, created := createPayments(ctx, client, base, pairs, *payments, *concurrency, *amount)
	printSummary("create payments", paymentResults)

	if *waitProcessing {
		processingResults := waitPaymentsProcessed(ctx, client, base, created, *concurrency)
		printSummary("wait processing", processingResults)
	}

	var failed int
	for _, item := range registerResults {
		if item.err != nil || item.statusCode >= 400 {
			failed++
		}
	}
	for _, item := range paymentResults {
		if item.err != nil || item.statusCode >= 400 {
			failed++
		}
	}
	if failed > 0 {
		log.Fatalf("load test finished with %d failed operations", failed)
	}
}

func createUserPairs(ctx context.Context, client *http.Client, base string, runID int64, count int) ([]authResponse, []result, error) {
	pairs := make([]authResponse, 0, count*2)
	results := make([]result, 0, count*2)
	for i := 0; i < count; i++ {
		recipient, recipientResult := register(ctx, client, base, map[string]any{
			"email":     fmt.Sprintf("load-recipient-%d-%d@test.local", runID, i),
			"password":  "123456",
			"full_name": "Load Recipient",
			"role":      "CLIENT",
			"balance":   0,
		})
		results = append(results, recipientResult)
		if recipientResult.err != nil || recipientResult.statusCode >= 400 {
			return nil, results, fmt.Errorf("recipient registration failed: %v", recipientResult.err)
		}

		sender, senderResult := register(ctx, client, base, map[string]any{
			"email":     fmt.Sprintf("load-sender-%d-%d@test.local", runID, i),
			"password":  "123456",
			"full_name": "Load Sender",
			"role":      "CLIENT",
			"balance":   1_000_000_000,
		})
		results = append(results, senderResult)
		if senderResult.err != nil || senderResult.statusCode >= 400 {
			return nil, results, fmt.Errorf("sender registration failed: %v", senderResult.err)
		}

		pairs = append(pairs, sender, recipient)
	}
	return pairs, results, nil
}

func createPayments(ctx context.Context, client *http.Client, base string, pairs []authResponse, count int, concurrency int, amount int64) ([]result, []createdPayment) {
	jobs := make(chan int)
	results := make([]result, 0, count)
	created := make([]createdPayment, 0, count)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				pairIndex := (index % (len(pairs) / 2)) * 2
				sender := pairs[pairIndex]
				recipient := pairs[pairIndex+1]
				payment, item := postPayment(ctx, client, base, sender.Token, map[string]any{
					"recipient_id": recipient.User.ID,
					"amount":       amount,
					"description":  "load test payment",
					"payment_type": "SINGLE",
				})

				mu.Lock()
				results = append(results, item)
				if item.err == nil && item.statusCode < 400 && payment.ID != 0 {
					created = append(created, createdPayment{ID: payment.ID, Token: sender.Token})
				}
				mu.Unlock()
			}
		}()
	}

	for i := 0; i < count; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return results, created
}

func waitPaymentsProcessed(ctx context.Context, client *http.Client, base string, payments []createdPayment, concurrency int) []result {
	jobs := make(chan createdPayment)
	results := make([]result, 0, len(payments))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				res := waitPaymentProcessed(ctx, client, base, item)
				mu.Lock()
				results = append(results, res)
				mu.Unlock()
			}
		}()
	}

	for _, item := range payments {
		jobs <- item
	}
	close(jobs)
	wg.Wait()
	return results
}

func register(ctx context.Context, client *http.Client, base string, payload map[string]any) (authResponse, result) {
	var out authResponse
	res := doJSON(ctx, client, http.MethodPost, base+"/api/auth/register", nil, payload, &out)
	res.name = "register"
	return out, res
}

func postPayment(ctx context.Context, client *http.Client, base string, token string, payload map[string]any) (payment, result) {
	var out payment
	res := doJSON(ctx, client, http.MethodPost, base+"/api/payments", bearer(token), payload, &out)
	res.name = "create_payment"
	return out, res
}

func waitPaymentProcessed(ctx context.Context, client *http.Client, base string, item createdPayment) result {
	start := time.Now()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		var out payment
		res := doJSON(ctx, client, http.MethodGet, fmt.Sprintf("%s/api/payments/%d", base, item.ID), bearer(item.Token), nil, &out)
		if res.err != nil || res.statusCode >= 400 {
			res.name = "wait_processing"
			res.duration = time.Since(start)
			return res
		}
		if out.Status != "PENDING" || out.ProcessedAt != "" {
			return result{name: "wait_processing", statusCode: http.StatusOK, duration: time.Since(start)}
		}

		select {
		case <-ctx.Done():
			return result{name: "wait_processing", statusCode: http.StatusRequestTimeout, duration: time.Since(start), err: ctx.Err()}
		case <-ticker.C:
		}
	}
}

type createdPayment struct {
	ID    int64
	Token string
}

func doJSON(ctx context.Context, client *http.Client, method string, url string, headers http.Header, payload any, out any) result {
	start := time.Now()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return result{statusCode: 0, duration: time.Since(start), err: err}
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return result{statusCode: 0, duration: time.Since(start), err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return result{statusCode: 0, duration: time.Since(start), err: err}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 400 {
		return result{statusCode: resp.StatusCode, duration: time.Since(start), err: fmt.Errorf("%s", strings.TrimSpace(string(raw)))}
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return result{statusCode: resp.StatusCode, duration: time.Since(start), err: err}
		}
	}
	return result{statusCode: resp.StatusCode, duration: time.Since(start)}
}

func waitForAPI(ctx context.Context, client *http.Client, base string) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func printSummary(name string, results []result) {
	if len(results) == 0 {
		fmt.Printf("%s: no results\n", name)
		return
	}

	durations := make([]time.Duration, 0, len(results))
	statuses := make(map[int]int)
	var failed int
	for _, item := range results {
		durations = append(durations, item.duration)
		statuses[item.statusCode]++
		if item.err != nil || item.statusCode >= 400 {
			failed++
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })

	fmt.Printf(
		"%s: total=%d failed=%d min=%s p50=%s p95=%s max=%s statuses=%v\n",
		name,
		len(results),
		failed,
		durations[0].Round(time.Millisecond),
		percentile(durations, 50).Round(time.Millisecond),
		percentile(durations, 95).Round(time.Millisecond),
		durations[len(durations)-1].Round(time.Millisecond),
		statuses,
	)
}

func percentile(values []time.Duration, p int) time.Duration {
	if len(values) == 0 {
		return 0
	}
	index := (len(values)*p + 99) / 100
	if index <= 0 {
		index = 1
	}
	if index > len(values) {
		index = len(values)
	}
	return values[index-1]
}

func bearer(token string) http.Header {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+token)
	return headers
}

func env(key, fallback string) string {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}

func envInt64(key string, fallback int64) int64 {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return fallback
	}
	var parsed int64
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}

func envBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(getenv(key)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes"
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getenv(key string) string {
	return os.Getenv(key)
}
