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

func TestCreatePaymentHoldsSenderOperationsOnReviewFraud(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := openTestStore(t, ctx)

	sender := createFraudTestUser(t, ctx, s, "review", 10_000_000)
	recipient := createFraudTestUser(t, ctx, s, "recipient", 0)
	otherRecipient := createFraudTestUser(t, ctx, s, "other-recipient", 0)
	banker := createFraudTestUser(t, ctx, s, "banker-review", 0)
	backdateUser(t, ctx, s, sender.ID, 2*time.Hour)
	setUserRole(t, ctx, s, banker.ID, RoleBanker)

	t.Cleanup(func() {
		cleanupFraudTestUsers(t, ctx, s, sender.ID, recipient.ID, otherRecipient.ID, banker.ID)
	})

	var reviewPayment Payment
	for i := 0; i < 5; i++ {
		payment, err := s.CreatePayment(ctx, Payment{
			SenderID:    sender.ID,
			RecipientID: recipient.ID,
			Amount:      10_000,
			PaymentType: "SINGLE",
			Description: fmt.Sprintf("review repeated payment %d", i+1),
		})
		if err != nil {
			t.Fatalf("CreatePayment #%d returned error: %v", i+1, err)
		}
		reviewPayment = payment
	}

	if reviewPayment.Status != StatusPending {
		t.Fatalf("review payment status = %q, want %q", reviewPayment.Status, StatusPending)
	}
	if reviewPayment.FraudScore < fraudReviewScore || reviewPayment.FraudScore >= fraudCriticalScore {
		t.Fatalf("review payment fraud score = %d, want review range", reviewPayment.FraudScore)
	}

	info, err := s.GetBlockInfo(ctx, sender.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.IsBlocked {
		t.Fatalf("sender should not be hard-blocked on review fraud: %+v", info)
	}
	if info.OperationHoldPaymentID == nil || *info.OperationHoldPaymentID != reviewPayment.ID {
		t.Fatalf("operation hold payment id = %v, want %d", info.OperationHoldPaymentID, reviewPayment.ID)
	}
	if info.OperationHoldAt == nil || !strings.Contains(info.OperationHoldReason, "review") {
		t.Fatalf("operation hold info = %+v, want review hold", info)
	}
	if info.SuspiciousPayments != 1 {
		t.Fatalf("suspicious payments = %d, want 1", info.SuspiciousPayments)
	}
	if info.RejectedPayments != 0 {
		t.Fatalf("rejected payments = %d, want 0", info.RejectedPayments)
	}

	_, err = s.CreatePayment(ctx, Payment{
		SenderID:    sender.ID,
		RecipientID: otherRecipient.ID,
		Amount:      10_000,
		PaymentType: "SINGLE",
		Description: "payment while banker review is pending",
	})
	if err == nil || err.Error() != "sender has payment pending banker review" {
		t.Fatalf("CreatePayment during review hold error = %v, want hold error", err)
	}

	if err := s.DecidePayment(ctx, reviewPayment.ID, banker.ID, StatusApproved, ""); err != nil {
		t.Fatalf("DecidePayment approve returned error: %v", err)
	}

	if _, err := s.CreatePayment(ctx, Payment{
		SenderID:    sender.ID,
		RecipientID: otherRecipient.ID,
		Amount:      10_000,
		PaymentType: "SINGLE",
		Description: "payment after banker approval",
	}); err != nil {
		t.Fatalf("CreatePayment after banker approval returned error: %v", err)
	}
}

func TestCreatePaymentBlocksSenderAndExposesBlockInfoOnCriticalFraud(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := openTestStore(t, ctx)

	sender := createFraudTestUser(t, ctx, s, "blocked", 10_000_000)
	recipient := createFraudTestUser(t, ctx, s, "recipient", 0)

	t.Cleanup(func() {
		cleanupFraudTestUsers(t, ctx, s, sender.ID, recipient.ID)
	})

	blockedPayment, err := s.CreatePayment(ctx, Payment{
		SenderID:    sender.ID,
		RecipientID: recipient.ID,
		Amount:      160_000,
		PaymentType: "SINGLE",
		Description: "critical fresh large payment",
	})
	if err != nil {
		t.Fatalf("CreatePayment returned error: %v", err)
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

func TestUnblockUserResetsFraudHistoryCutoff(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := openTestStore(t, ctx)

	sender := createFraudTestUser(t, ctx, s, "reset", 10_000_000)
	recipient := createFraudTestUser(t, ctx, s, "recipient-reset", 0)
	admin := createFraudTestUser(t, ctx, s, "admin-reset", 0)
	backdateUser(t, ctx, s, sender.ID, 2*time.Hour)
	backdateUser(t, ctx, s, admin.ID, 2*time.Hour)
	setUserRole(t, ctx, s, admin.ID, RoleAdmin)

	t.Cleanup(func() {
		cleanupFraudTestUsers(t, ctx, s, sender.ID, recipient.ID, admin.ID)
	})

	for i := 0; i < 5; i++ {
		if _, err := s.CreatePayment(ctx, Payment{
			SenderID:    sender.ID,
			RecipientID: recipient.ID,
			Amount:      10_000,
			PaymentType: "SINGLE",
			Description: fmt.Sprintf("pre unblock payment %d", i+1),
		}); err != nil {
			t.Fatalf("CreatePayment before unblock #%d returned error: %v", i+1, err)
		}
	}

	if _, err := s.UnblockUser(ctx, sender.ID, admin.ID); err != nil {
		t.Fatalf("UnblockUser returned error: %v", err)
	}
	info, err := s.GetBlockInfo(ctx, sender.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.IsBlocked {
		t.Fatalf("sender is still blocked after unblock: %+v", info)
	}
	if info.SuspiciousPayments != 0 || info.RejectedPayments != 0 || len(info.SuspiciousOperations) != 0 {
		t.Fatalf("fraud info after unblock = %+v, want reset counters", info)
	}

	payment, err := s.CreatePayment(ctx, Payment{
		SenderID:    sender.ID,
		RecipientID: recipient.ID,
		Amount:      10_000,
		PaymentType: "SINGLE",
		Description: "payment after unblock",
	})
	if err != nil {
		t.Fatalf("CreatePayment after unblock returned error: %v", err)
	}
	if payment.Status != StatusPending {
		t.Fatalf("payment after unblock status = %q, want %q", payment.Status, StatusPending)
	}
	if payment.FraudScore != 0 {
		t.Fatalf("payment after unblock fraud score = %d, want 0", payment.FraudScore)
	}
}

func TestUnblockUserResetsLimitHistoryCutoff(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := openTestStore(t, ctx)

	sender := createFraudTestUser(t, ctx, s, "limit-reset", 10_000_000)
	recipient := createFraudTestUser(t, ctx, s, "limit-recipient", 0)
	admin := createFraudTestUser(t, ctx, s, "limit-admin", 0)
	backdateUser(t, ctx, s, sender.ID, 2*time.Hour)
	setUserRole(t, ctx, s, admin.ID, RoleAdmin)

	t.Cleanup(func() {
		cleanupFraudTestUsers(t, ctx, s, sender.ID, recipient.ID, admin.ID)
	})

	if _, err := s.UpdateUserLimits(ctx, sender.ID, 100_000, 1_000_000); err != nil {
		t.Fatalf("UpdateUserLimits returned error: %v", err)
	}
	if _, err := s.CreatePayment(ctx, Payment{
		SenderID:    sender.ID,
		RecipientID: recipient.ID,
		Amount:      80_000,
		PaymentType: "SINGLE",
		Description: "uses most daily limit",
	}); err != nil {
		t.Fatalf("CreatePayment before limit reset returned error: %v", err)
	}
	if _, err := s.CreatePayment(ctx, Payment{
		SenderID:    sender.ID,
		RecipientID: recipient.ID,
		Amount:      30_000,
		PaymentType: "SINGLE",
		Description: "should exceed daily limit",
	}); err == nil || err.Error() != "daily limit exceeded" {
		t.Fatalf("CreatePayment before unblock error = %v, want daily limit exceeded", err)
	}

	if _, err := s.UnblockUser(ctx, sender.ID, admin.ID); err != nil {
		t.Fatalf("UnblockUser returned error: %v", err)
	}
	if _, err := s.CreatePayment(ctx, Payment{
		SenderID:    sender.ID,
		RecipientID: recipient.ID,
		Amount:      30_000,
		PaymentType: "SINGLE",
		Description: "allowed after limit reset",
	}); err != nil {
		t.Fatalf("CreatePayment after limit reset returned error: %v", err)
	}
}

func TestAdminBlockAndClearHoldCreateLocalizedNotifications(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := openTestStore(t, ctx)

	client := createFraudTestUser(t, ctx, s, "notify-client", 10_000_000)
	admin := createFraudTestUser(t, ctx, s, "notify-admin", 0)
	setUserRole(t, ctx, s, admin.ID, RoleAdmin)

	t.Cleanup(func() {
		cleanupFraudTestUsers(t, ctx, s, client.ID, admin.ID)
	})

	if _, err := s.BlockUser(ctx, client.ID, admin.ID, "Проверка документов"); err != nil {
		t.Fatalf("BlockUser returned error: %v", err)
	}
	notifications, err := s.Notifications(ctx, client.ID, false, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if notifications.Total != 1 || notifications.Items[0].Type != "USER_BLOCKED" || notifications.Items[0].Body != "Проверка документов" {
		t.Fatalf("block notifications = %+v, want localized USER_BLOCKED", notifications)
	}

	if _, err := s.ClearUserOperationHold(ctx, client.ID, admin.ID, ""); err != nil {
		t.Fatalf("ClearUserOperationHold returned error: %v", err)
	}
	notifications, err = s.Notifications(ctx, client.ID, false, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if notifications.Total != 2 || notifications.Items[0].Type != "HOLD_CLEARED" || notifications.Items[0].Body != "Ограничение операций снято администратором" {
		t.Fatalf("clear-hold notifications = %+v, want localized HOLD_CLEARED", notifications)
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

func setUserRole(t *testing.T, ctx context.Context, s *Store, userID int64, role string) {
	t.Helper()

	_, err := s.pool.Exec(ctx, `
		update users set role=$2 where id=$1
	`, userID, role)
	if err != nil {
		t.Fatalf("failed to set user %d role: %v", userID, err)
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
