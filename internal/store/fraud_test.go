package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCreatePaymentMarksRepeatedPaymentsSuspiciousWithoutBlockingBelowCriticalScore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := openTestStore(t, ctx)

	sender := createFraudTestUser(t, ctx, s, "suspicious", 10_000_000)
	recipient := createFraudTestUser(t, ctx, s, "recipient", 0)
	backdateUser(t, ctx, s, sender.ID, 2*time.Hour)

	t.Cleanup(func() {
		cleanupFraudTestUsers(t, ctx, s, sender.ID, recipient.ID)
	})

	var third Payment
	for i := 0; i < 3; i++ {
		payment, err := s.CreatePayment(ctx, Payment{
			SenderID:    sender.ID,
			RecipientID: recipient.ID,
			Amount:      10_000,
			PaymentType: "SINGLE",
			Description: fmt.Sprintf("below critical repeated payment %d", i+1),
		})
		if err != nil {
			t.Fatalf("CreatePayment #%d returned error: %v", i+1, err)
		}
		third = payment
	}

	if third.Status != StatusPending {
		t.Fatalf("third payment status = %q, want %q", third.Status, StatusPending)
	}
	if third.FraudScore != 35 {
		t.Fatalf("third payment fraud score = %d, want 35", third.FraudScore)
	}

	info, err := s.GetBlockInfo(ctx, sender.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.IsBlocked {
		t.Fatalf("sender should not be blocked below critical fraud score: %+v", info)
	}
	if info.SuspiciousPayments != 0 {
		t.Fatalf("suspicious payments = %d, want 0 because review threshold is 50", info.SuspiciousPayments)
	}
}

func TestCreatePaymentBlocksSenderAndExposesBlockInfoOnCriticalFraud(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := openTestStore(t, ctx)

	sender := createFraudTestUser(t, ctx, s, "blocked", 10_000_000)
	recipient := createFraudTestUser(t, ctx, s, "recipient", 0)
	backdateUser(t, ctx, s, sender.ID, 2*time.Hour)

	t.Cleanup(func() {
		cleanupFraudTestUsers(t, ctx, s, sender.ID, recipient.ID)
	})

	var blockedPayment Payment
	for i := 0; i < 5; i++ {
		payment, err := s.CreatePayment(ctx, Payment{
			SenderID:    sender.ID,
			RecipientID: recipient.ID,
			Amount:      10_000,
			PaymentType: "SINGLE",
			Description: fmt.Sprintf("critical repeated payment %d", i+1),
		})
		if err != nil {
			t.Fatalf("CreatePayment #%d returned error: %v", i+1, err)
		}
		blockedPayment = payment
	}

	if blockedPayment.Status != StatusRejected {
		t.Fatalf("blocked payment status = %q, want %q", blockedPayment.Status, StatusRejected)
	}
	if blockedPayment.FraudScore < fraudCriticalScore {
		t.Fatalf("blocked payment fraud score = %d, want at least %d", blockedPayment.FraudScore, fraudCriticalScore)
	}
	if !strings.Contains(blockedPayment.RejectionReason, "fraud detected") {
		t.Fatalf("blocked payment rejection reason = %q, want fraud reason", blockedPayment.RejectionReason)
	}
	if blockedPayment.ProcessedAt == nil {
		t.Fatal("blocked payment processed_at is nil, want immediate rejection timestamp")
	}

	info, err := s.GetBlockInfo(ctx, sender.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsBlocked {
		t.Fatalf("sender is not blocked: %+v", info)
	}
	if info.BlockReason == "" || !strings.Contains(info.BlockReason, "fraud score") {
		t.Fatalf("block reason = %q, want fraud score details", info.BlockReason)
	}
	if info.BlockedAt == nil {
		t.Fatal("blocked_at is nil")
	}
	if info.SuspiciousPayments != 1 {
		t.Fatalf("suspicious payments = %d, want 1", info.SuspiciousPayments)
	}
	if info.RejectedPayments != 1 {
		t.Fatalf("rejected payments = %d, want 1", info.RejectedPayments)
	}
	if len(info.SuspiciousOperations) == 0 {
		t.Fatal("suspicious operations is empty")
	}
	if info.SuspiciousOperations[0].ID != blockedPayment.ID {
		t.Fatalf("first suspicious operation id = %d, want %d", info.SuspiciousOperations[0].ID, blockedPayment.ID)
	}

	_, err = s.CreatePayment(ctx, Payment{
		SenderID:    sender.ID,
		RecipientID: recipient.ID,
		Amount:      10_000,
		PaymentType: "SINGLE",
		Description: "payment after fraud block",
	})
	if err == nil || err.Error() != "sender is blocked" {
		t.Fatalf("CreatePayment after block error = %v, want sender is blocked", err)
	}
}

func openTestStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()

	databaseURL := os.Getenv("STORE_TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		databaseURL = "postgres://bank_user:bank_password@localhost:5432/bank_processing?sslmode=disable"
	}

	s, err := Open(ctx, databaseURL)
	if err != nil {
		t.Skipf("Postgres is not available for store fraud tests: %v", err)
	}
	t.Cleanup(s.Close)

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}
	return s
}

func createFraudTestUser(t *testing.T, ctx context.Context, s *Store, label string, balance int64) User {
	t.Helper()

	suffix := fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Nanosecond())
	user, err := s.CreateUser(ctx, User{
		Email:        fmt.Sprintf("fraud-%s-%s@test.local", label, suffix),
		PasswordHash: "test-password-hash",
		FullName:     fmt.Sprintf("Fraud Test %s", label),
		Role:         RoleClient,
		Balance:      balance,
		DailyLimit:   10_000_000,
		MonthlyLimit: 100_000_000,
	})
	if err != nil {
		t.Fatalf("CreateUser(%s) failed: %v", label, err)
	}
	return user
}

func backdateUser(t *testing.T, ctx context.Context, s *Store, userID int64, age time.Duration) {
	t.Helper()

	_, err := s.pool.Exec(ctx, `
		update users
		set created_at = now() - $2::interval
		where id = $1
	`, userID, fmt.Sprintf("%d seconds", int64(age.Seconds())))
	if err != nil {
		t.Fatalf("failed to backdate user %d: %v", userID, err)
	}
}

func cleanupFraudTestUsers(t *testing.T, ctx context.Context, s *Store, userIDs ...int64) {
	t.Helper()

	_, err := s.pool.Exec(ctx, `
		delete from payment_transaction_log
		where payment_id in (
			select id from payments where sender_id = any($1) or recipient_id = any($1)
		)
	`, userIDs)
	if err != nil {
		t.Errorf("failed to cleanup payment transaction log: %v", err)
	}
	_, err = s.pool.Exec(ctx, `
		delete from integration_requests
		where payment_id in (
			select id from payments where sender_id = any($1) or recipient_id = any($1)
		)
	`, userIDs)
	if err != nil {
		t.Errorf("failed to cleanup integration requests: %v", err)
	}
	_, err = s.pool.Exec(ctx, `
		delete from payment_status_history
		where payment_id in (
			select id from payments where sender_id = any($1) or recipient_id = any($1)
		)
	`, userIDs)
	if err != nil {
		t.Errorf("failed to cleanup payment status history: %v", err)
	}
	_, err = s.pool.Exec(ctx, `
		delete from audit_log where user_id = any($1)
	`, userIDs)
	if err != nil {
		t.Errorf("failed to cleanup audit log: %v", err)
	}
	_, err = s.pool.Exec(ctx, `
		delete from payments where sender_id = any($1) or recipient_id = any($1)
	`, userIDs)
	if err != nil {
		t.Errorf("failed to cleanup payments: %v", err)
	}
	_, err = s.pool.Exec(ctx, `
		delete from users where id = any($1)
	`, userIDs)
	if err != nil {
		t.Errorf("failed to cleanup users: %v", err)
	}
}
