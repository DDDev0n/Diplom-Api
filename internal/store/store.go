package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	RoleClient = "CLIENT"
	RoleBanker = "BANKER"
	RoleAdmin  = "ADMIN"

	StatusPending   = "PENDING"
	StatusApproved  = "APPROVED"
	StatusRejected  = "REJECTED"
	StatusCompleted = "COMPLETED"
	StatusCancelled = "CANCELLED"
)

type Store struct {
	pool *pgxpool.Pool
}

type User struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	FullName     string    `json:"full_name"`
	Phone        string    `json:"phone,omitempty"`
	Role         string    `json:"role"`
	Balance      int64     `json:"balance"`
	DailyLimit   int64     `json:"daily_limit"`
	MonthlyLimit int64     `json:"monthly_limit"`
	IsBlocked    bool      `json:"is_blocked"`
	CreatedAt    time.Time `json:"created_at"`
}

type Payment struct {
	ID              int64      `json:"id"`
	SenderID        int64      `json:"sender_id"`
	RecipientID     int64      `json:"recipient_id"`
	Amount          int64      `json:"amount"`
	Commission      int64      `json:"commission"`
	Status          string     `json:"status"`
	PaymentType     string     `json:"payment_type"`
	Description     string     `json:"description,omitempty"`
	FraudScore      int        `json:"fraud_score"`
	ApprovedBy      *int64     `json:"approved_by,omitempty"`
	RejectionReason string     `json:"rejection_reason,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	ProcessedAt     *time.Time `json:"processed_at,omitempty"`
}

type Template struct {
	ID            int64  `json:"id"`
	UserID        int64  `json:"user_id"`
	TemplateName  string `json:"template_name"`
	RecipientID   int64  `json:"recipient_id"`
	DefaultAmount int64  `json:"default_amount"`
	Description   string `json:"description,omitempty"`
}

type Notification struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	IsRead    bool      `json:"is_read"`
	CreatedAt time.Time `json:"created_at"`
}

type UserSearch struct {
	Query string
	Role  string
	Limit int
}

type ClientStats struct {
	SentCount        int64 `json:"sent_count"`
	ReceivedCount    int64 `json:"received_count"`
	SentAmount       int64 `json:"sent_amount"`
	ReceivedAmount   int64 `json:"received_amount"`
	PendingPayments  int64 `json:"pending_payments"`
	ApprovedPayments int64 `json:"approved_payments"`
	RejectedPayments int64 `json:"rejected_payments"`
}

type ClientProfile struct {
	User     User        `json:"user"`
	Stats    ClientStats `json:"stats"`
	Payments []Payment   `json:"payments"`
}

type PaymentStats struct {
	Total     int64 `json:"total"`
	Pending   int64 `json:"pending"`
	Approved  int64 `json:"approved"`
	Rejected  int64 `json:"rejected"`
	Completed int64 `json:"completed"`
	Cancelled int64 `json:"cancelled"`
}

type BankerStats struct {
	BankerID     int64      `json:"banker_id"`
	Approved     int64      `json:"approved"`
	Rejected     int64      `json:"rejected"`
	Total        int64      `json:"total"`
	LastDecision *time.Time `json:"last_decision,omitempty"`
}

type BankerStatsRow struct {
	Banker       User       `json:"banker"`
	Approved     int64      `json:"approved"`
	Rejected     int64      `json:"rejected"`
	Total        int64      `json:"total"`
	LastDecision *time.Time `json:"last_decision,omitempty"`
}

type AuditEntry struct {
	ID         int64     `json:"id"`
	UserID     *int64    `json:"user_id,omitempty"`
	Action     string    `json:"action"`
	EntityType string    `json:"entity_type"`
	EntityID   *int64    `json:"entity_id,omitempty"`
	Details    string    `json:"details,omitempty"`
	IPAddress  string    `json:"ip_address,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, schemaSQL)
	return err
}

func (s *Store) CreateUser(ctx context.Context, user User) (User, error) {
	err := s.pool.QueryRow(ctx, `
		insert into users (email, password_hash, full_name, phone, role, balance_cents, daily_limit_cents, monthly_limit_cents)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
		returning id, created_at
	`, user.Email, user.PasswordHash, user.FullName, user.Phone, user.Role, user.Balance, user.DailyLimit, user.MonthlyLimit).Scan(&user.ID, &user.CreatedAt)
	return user, err
}

func (s *Store) CountUsersByRole(ctx context.Context, role string) (int64, error) {
	var count int64
	err := s.pool.QueryRow(ctx, `select count(*) from users where role=$1`, role).Scan(&count)
	return count, err
}

func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	return scanUser(s.pool.QueryRow(ctx, `
		select id, email, password_hash, full_name, coalesce(phone, ''), role, balance_cents, daily_limit_cents, monthly_limit_cents, is_blocked, created_at
		from users where email=$1
	`, email))
}

func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	return scanUser(s.pool.QueryRow(ctx, `
		select id, email, password_hash, full_name, coalesce(phone, ''), role, balance_cents, daily_limit_cents, monthly_limit_cents, is_blocked, created_at
		from users where id=$1
	`, id))
}

func (s *Store) SearchUsers(ctx context.Context, filter UserSearch) ([]User, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := "%" + filter.Query + "%"

	rows, err := s.pool.Query(ctx, `
		select id, email, password_hash, full_name, coalesce(phone, ''), role, balance_cents, daily_limit_cents, monthly_limit_cents, is_blocked, created_at
		from users
		where ($1 = '' or role = $1)
		  and ($2 = '%%' or email ilike $2 or full_name ilike $2 or coalesce(phone, '') ilike $2 or id::text = trim(both '%' from $2))
		order by created_at desc
		limit $3
	`, filter.Role, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Store) ClientProfile(ctx context.Context, clientID int64) (ClientProfile, error) {
	user, err := s.UserByID(ctx, clientID)
	if err != nil {
		return ClientProfile{}, err
	}

	var stats ClientStats
	err = s.pool.QueryRow(ctx, `
		select
			count(*) filter (where sender_id=$1),
			count(*) filter (where recipient_id=$1),
			coalesce(sum(amount_cents) filter (where sender_id=$1), 0),
			coalesce(sum(amount_cents) filter (where recipient_id=$1), 0),
			count(*) filter (where status=$2),
			count(*) filter (where status=$3),
			count(*) filter (where status=$4)
		from payments
		where sender_id=$1 or recipient_id=$1
	`, clientID, StatusPending, StatusApproved, StatusRejected).Scan(
		&stats.SentCount,
		&stats.ReceivedCount,
		&stats.SentAmount,
		&stats.ReceivedAmount,
		&stats.PendingPayments,
		&stats.ApprovedPayments,
		&stats.RejectedPayments,
	)
	if err != nil {
		return ClientProfile{}, err
	}

	payments, err := s.PaymentsForUser(ctx, clientID, 100)
	if err != nil {
		return ClientProfile{}, err
	}
	return ClientProfile{User: user, Stats: stats, Payments: payments}, nil
}

func (s *Store) CreatePayment(ctx context.Context, payment Payment) (Payment, error) {
	payment.Status = StatusPending

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return payment, err
	}
	defer tx.Rollback(ctx)

	var sender User
	err = tx.QueryRow(ctx, `
		select id, email, password_hash, full_name, coalesce(phone, ''), role, balance_cents, daily_limit_cents, monthly_limit_cents, is_blocked, created_at
		from users where id=$1 for update
	`, payment.SenderID).Scan(&sender.ID, &sender.Email, &sender.PasswordHash, &sender.FullName, &sender.Phone, &sender.Role, &sender.Balance, &sender.DailyLimit, &sender.MonthlyLimit, &sender.IsBlocked, &sender.CreatedAt)
	if err != nil {
		return payment, err
	}
	if sender.IsBlocked {
		return payment, errors.New("sender is blocked")
	}
	if sender.Balance < payment.Amount+payment.Commission {
		return payment, errors.New("insufficient balance")
	}

	err = tx.QueryRow(ctx, `
		insert into payments (sender_id, recipient_id, amount_cents, commission_cents, status, payment_type, description)
		values ($1, $2, $3, $4, $5, $6, $7)
		returning id, fraud_score, created_at
	`, payment.SenderID, payment.RecipientID, payment.Amount, payment.Commission, payment.Status, payment.PaymentType, payment.Description).
		Scan(&payment.ID, &payment.FraudScore, &payment.CreatedAt)
	if err != nil {
		return payment, err
	}

	_, err = tx.Exec(ctx, `
		insert into audit_log (user_id, action, entity_type, entity_id, details)
		values ($1, 'CREATE_PAYMENT', 'payment', $2, $3)
	`, payment.SenderID, payment.ID, fmt.Sprintf("amount=%d", payment.Amount))
	if err != nil {
		return payment, err
	}

	return payment, tx.Commit(ctx)
}

func (s *Store) GetPayment(ctx context.Context, id int64) (Payment, error) {
	return scanPayment(s.pool.QueryRow(ctx, `
		select id, sender_id, recipient_id, amount_cents, commission_cents, status, payment_type,
		       coalesce(description, ''), fraud_score, approved_by, coalesce(rejection_reason, ''),
		       created_at, processed_at
		from payments where id=$1
	`, id))
}

func (s *Store) ListPayments(ctx context.Context, user User) ([]Payment, error) {
	query := `
		select id, sender_id, recipient_id, amount_cents, commission_cents, status, payment_type,
		       coalesce(description, ''), fraud_score, approved_by, coalesce(rejection_reason, ''),
		       created_at, processed_at
		from payments
		where sender_id=$1 or recipient_id=$1
		order by created_at desc
		limit 100
	`
	rows, err := s.pool.Query(ctx, query, user.ID)
	if user.Role == RoleBanker || user.Role == RoleAdmin {
		rows, err = s.pool.Query(ctx, `
			select id, sender_id, recipient_id, amount_cents, commission_cents, status, payment_type,
			       coalesce(description, ''), fraud_score, approved_by, coalesce(rejection_reason, ''),
			       created_at, processed_at
			from payments order by created_at desc limit 200
		`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPayments(rows)
}

func (s *Store) PendingPayments(ctx context.Context) ([]Payment, error) {
	rows, err := s.pool.Query(ctx, `
		select id, sender_id, recipient_id, amount_cents, commission_cents, status, payment_type,
		       coalesce(description, ''), fraud_score, approved_by, coalesce(rejection_reason, ''),
		       created_at, processed_at
		from payments where status=$1 order by created_at asc limit 100
	`, StatusPending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPayments(rows)
}

func (s *Store) PaymentsForUser(ctx context.Context, userID int64, limit int) ([]Payment, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		select id, sender_id, recipient_id, amount_cents, commission_cents, status, payment_type,
		       coalesce(description, ''), fraud_score, approved_by, coalesce(rejection_reason, ''),
		       created_at, processed_at
		from payments
		where sender_id=$1 or recipient_id=$1
		order by created_at desc
		limit $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPayments(rows)
}

func (s *Store) ApplyProcessingResult(ctx context.Context, paymentID int64, status string, fraudScore int, reason string) error {
	_, err := s.pool.Exec(ctx, `
		update payments
		set status=$2, fraud_score=$3, rejection_reason=nullif($4, ''), processed_at=now()
		where id=$1 and status=$5
	`, paymentID, status, fraudScore, reason, StatusPending)
	return err
}

func (s *Store) DecidePayment(ctx context.Context, paymentID, bankerID int64, status, reason string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		update payments
		set status=$1, approved_by=$2, rejection_reason=nullif($3, ''), processed_at=now()
		where id=$4 and status=$5
	`, status, bankerID, reason, paymentID, StatusPending)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("payment is not pending")
	}

	_, err = tx.Exec(ctx, `
		insert into audit_log (user_id, action, entity_type, entity_id, details)
		values ($1, $2, 'payment', $3, $4)
	`, bankerID, "BANKER_"+status, paymentID, reason)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) PaymentStats(ctx context.Context) (PaymentStats, error) {
	var stats PaymentStats
	err := s.pool.QueryRow(ctx, `
		select
			count(*),
			count(*) filter (where status=$1),
			count(*) filter (where status=$2),
			count(*) filter (where status=$3),
			count(*) filter (where status=$4),
			count(*) filter (where status=$5)
		from payments
	`, StatusPending, StatusApproved, StatusRejected, StatusCompleted, StatusCancelled).Scan(
		&stats.Total,
		&stats.Pending,
		&stats.Approved,
		&stats.Rejected,
		&stats.Completed,
		&stats.Cancelled,
	)
	return stats, err
}

func (s *Store) BankerStats(ctx context.Context, bankerID int64) (BankerStats, error) {
	var stats BankerStats
	stats.BankerID = bankerID
	err := s.pool.QueryRow(ctx, `
		select
			count(*) filter (where status=$2),
			count(*) filter (where status=$3),
			count(*),
			max(processed_at)
		from payments
		where approved_by=$1
	`, bankerID, StatusApproved, StatusRejected).Scan(&stats.Approved, &stats.Rejected, &stats.Total, &stats.LastDecision)
	return stats, err
}

func (s *Store) AllBankerStats(ctx context.Context) ([]BankerStatsRow, error) {
	rows, err := s.pool.Query(ctx, `
		select u.id, u.email, u.password_hash, u.full_name, coalesce(u.phone, ''), u.role,
		       u.balance_cents, u.daily_limit_cents, u.monthly_limit_cents, u.is_blocked, u.created_at,
		       count(p.id) filter (where p.status=$1),
		       count(p.id) filter (where p.status=$2),
		       count(p.id),
		       max(p.processed_at)
		from users u
		left join payments p on p.approved_by = u.id
		where u.role = $3
		group by u.id
		order by count(p.id) desc, u.created_at desc
	`, StatusApproved, StatusRejected, RoleBanker)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]BankerStatsRow, 0)
	for rows.Next() {
		var item BankerStatsRow
		if err := rows.Scan(
			&item.Banker.ID,
			&item.Banker.Email,
			&item.Banker.PasswordHash,
			&item.Banker.FullName,
			&item.Banker.Phone,
			&item.Banker.Role,
			&item.Banker.Balance,
			&item.Banker.DailyLimit,
			&item.Banker.MonthlyLimit,
			&item.Banker.IsBlocked,
			&item.Banker.CreatedAt,
			&item.Approved,
			&item.Rejected,
			&item.Total,
			&item.LastDecision,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DecisionsByBanker(ctx context.Context, bankerID int64, limit int) ([]Payment, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		select id, sender_id, recipient_id, amount_cents, commission_cents, status, payment_type,
		       coalesce(description, ''), fraud_score, approved_by, coalesce(rejection_reason, ''),
		       created_at, processed_at
		from payments
		where approved_by=$1
		order by processed_at desc nulls last, created_at desc
		limit $2
	`, bankerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPayments(rows)
}

func (s *Store) AuditForUser(ctx context.Context, userID int64, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		select id, user_id, action, entity_type, entity_id, coalesce(details, ''), coalesce(ip_address, ''), created_at
		from audit_log
		where user_id=$1
		order by created_at desc
		limit $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]AuditEntry, 0)
	for rows.Next() {
		var item AuditEntry
		if err := rows.Scan(&item.ID, &item.UserID, &item.Action, &item.EntityType, &item.EntityID, &item.Details, &item.IPAddress, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Audit(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		select id, user_id, action, entity_type, entity_id, coalesce(details, ''), coalesce(ip_address, ''), created_at
		from audit_log
		order by created_at desc
		limit $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]AuditEntry, 0)
	for rows.Next() {
		var item AuditEntry
		if err := rows.Scan(&item.ID, &item.UserID, &item.Action, &item.EntityType, &item.EntityID, &item.Details, &item.IPAddress, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanUser(row pgx.Row) (User, error) {
	var user User
	err := row.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.FullName, &user.Phone, &user.Role, &user.Balance, &user.DailyLimit, &user.MonthlyLimit, &user.IsBlocked, &user.CreatedAt)
	return user, err
}

func scanPayment(row pgx.Row) (Payment, error) {
	var payment Payment
	err := row.Scan(&payment.ID, &payment.SenderID, &payment.RecipientID, &payment.Amount, &payment.Commission, &payment.Status, &payment.PaymentType, &payment.Description, &payment.FraudScore, &payment.ApprovedBy, &payment.RejectionReason, &payment.CreatedAt, &payment.ProcessedAt)
	return payment, err
}

func scanPayments(rows pgx.Rows) ([]Payment, error) {
	payments := make([]Payment, 0)
	for rows.Next() {
		payment, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		payments = append(payments, payment)
	}
	return payments, rows.Err()
}

const schemaSQL = `
create table if not exists users (
	id bigserial primary key,
	email varchar(255) unique not null,
	password_hash varchar(255) not null,
	full_name varchar(255) not null,
	phone varchar(20),
	role varchar(20) not null default 'CLIENT',
	balance_cents bigint not null default 0,
	daily_limit_cents bigint not null default 10000000,
	monthly_limit_cents bigint not null default 100000000,
	is_blocked boolean not null default false,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now()
);

create table if not exists payment_templates (
	id bigserial primary key,
	user_id bigint not null references users(id),
	template_name varchar(255) not null,
	recipient_id bigint not null references users(id),
	default_amount_cents bigint not null,
	description text,
	is_active boolean not null default true,
	created_at timestamptz not null default now()
);

create table if not exists payments (
	id bigserial primary key,
	sender_id bigint not null references users(id),
	recipient_id bigint not null references users(id),
	amount_cents bigint not null check (amount_cents > 0),
	commission_cents bigint not null default 0,
	status varchar(20) not null default 'PENDING',
	payment_type varchar(30) not null default 'SINGLE',
	description text,
	template_id bigint references payment_templates(id),
	fraud_score integer not null default 0,
	approved_by bigint references users(id),
	rejection_reason text,
	created_at timestamptz not null default now(),
	processed_at timestamptz
);

create table if not exists commissions (
	id bigserial primary key,
	payment_type varchar(50) not null,
	min_amount_cents bigint not null default 0,
	max_amount_cents bigint not null default 999999999999,
	fixed_fee_cents bigint not null default 0,
	percentage_fee numeric(5,2) not null default 0,
	is_active boolean not null default true
);

create table if not exists notifications (
	id bigserial primary key,
	user_id bigint not null references users(id),
	type varchar(50) not null,
	title varchar(255) not null,
	message text not null,
	is_read boolean not null default false,
	created_at timestamptz not null default now()
);

create table if not exists audit_log (
	id bigserial primary key,
	user_id bigint references users(id),
	action varchar(100) not null,
	entity_type varchar(50) not null,
	entity_id bigint,
	details text,
	ip_address varchar(45),
	created_at timestamptz not null default now()
);

create index if not exists idx_payments_sender on payments(sender_id);
create index if not exists idx_payments_recipient on payments(recipient_id);
create index if not exists idx_payments_status on payments(status);
create index if not exists idx_users_full_name on users(full_name);
`
