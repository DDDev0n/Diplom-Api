package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"bank-processing-data-subsystem/internal/auth"
	"bank-processing-data-subsystem/internal/config"
	"bank-processing-data-subsystem/internal/queue"
	"bank-processing-data-subsystem/internal/store"
)

type Server struct {
	cfg       config.Config
	store     *store.Store
	publisher *queue.Publisher
}

type contextKey string

const userKey contextKey = "user"

func NewServer(cfg config.Config, store *store.Store, publisher *queue.Publisher) Server {
	return Server{cfg: cfg, store: store, publisher: publisher}
}

func (s Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", s.metrics)
	mux.HandleFunc("GET /swagger", s.swaggerUI)
	mux.HandleFunc("GET /swagger/", s.swaggerUI)
	mux.HandleFunc("GET /openapi.json", s.openapiJSON)
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("POST /api/auth/login", s.login)

	mux.Handle("GET /api/auth/me", s.authenticated(http.HandlerFunc(s.me)))
	mux.Handle("POST /api/payments", s.authenticated(http.HandlerFunc(s.createPayment)))
	mux.Handle("GET /api/payments", s.authenticated(http.HandlerFunc(s.listPayments)))
	mux.Handle("GET /api/payments/{id}", s.authenticated(http.HandlerFunc(s.getPayment)))
	mux.Handle("GET /api/banker/queue", s.authenticated(s.requireRole(store.RoleBanker, store.RoleAdmin, http.HandlerFunc(s.bankerQueue))))
	mux.Handle("GET /api/banker/clients", s.authenticated(s.requireRole(store.RoleBanker, store.RoleAdmin, http.HandlerFunc(s.searchClients))))
	mux.Handle("GET /api/banker/clients/{id}", s.authenticated(s.requireRole(store.RoleBanker, store.RoleAdmin, http.HandlerFunc(s.clientProfile))))
	mux.Handle("GET /api/banker/stats", s.authenticated(s.requireRole(store.RoleBanker, store.RoleAdmin, http.HandlerFunc(s.bankerStats))))
	mux.Handle("GET /api/banker/history", s.authenticated(s.requireRole(store.RoleBanker, store.RoleAdmin, http.HandlerFunc(s.bankerHistory))))
	mux.Handle("POST /api/banker/approve/{id}", s.authenticated(s.requireRole(store.RoleBanker, store.RoleAdmin, http.HandlerFunc(s.approvePayment))))
	mux.Handle("POST /api/banker/reject/{id}", s.authenticated(s.requireRole(store.RoleBanker, store.RoleAdmin, http.HandlerFunc(s.rejectPayment))))
	mux.Handle("GET /api/admin/users", s.authenticated(s.requireRole(store.RoleAdmin, http.HandlerFunc(s.adminUsers))))
	mux.Handle("POST /api/admin/users", s.authenticated(s.requireRole(store.RoleAdmin, http.HandlerFunc(s.adminCreateUser))))
	mux.Handle("GET /api/admin/stats", s.authenticated(s.requireRole(store.RoleAdmin, http.HandlerFunc(s.adminStats))))
	mux.Handle("GET /api/admin/audit", s.authenticated(s.requireRole(store.RoleAdmin, http.HandlerFunc(s.adminAudit))))
	mux.Handle("GET /api/admin/bankers/{id}/history", s.authenticated(s.requireRole(store.RoleAdmin, http.HandlerFunc(s.adminBankerHistory))))
	mux.Handle("GET /api/admin/clients/{id}/history", s.authenticated(s.requireRole(store.RoleAdmin, http.HandlerFunc(s.adminClientHistory))))

	return withSecurityHeaders(withCORS(metricsMiddleware(mux)))
}

func (s Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s Server) swaggerUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(swaggerHTML))
}

func (s Server) openapiJSON(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(openapiSpec))
}

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	FullName string `json:"full_name"`
	Phone    string `json:"phone"`
	Role     string `json:"role"`
	Balance  int64  `json:"balance"`
}

func (s Server) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Email == "" || req.Password == "" || req.FullName == "" {
		writeError(w, http.StatusBadRequest, "email, password and full_name are required")
		return
	}
	role := store.RoleClient
	if req.Role != "" && req.Role != store.RoleClient && req.Role != store.RoleAdmin {
		writeError(w, http.StatusBadRequest, "invalid role")
		return
	}
	if req.Role == store.RoleAdmin {
		admins, err := s.store.CountUsersByRole(r.Context(), store.RoleAdmin)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to check admins")
			return
		}
		if admins > 0 {
			writeError(w, http.StatusForbidden, "admin users can only be created by an admin")
			return
		}
		role = store.RoleAdmin
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}
	user, err := s.store.CreateUser(r.Context(), store.User{
		Email:        req.Email,
		PasswordHash: hash,
		FullName:     req.FullName,
		Phone:        req.Phone,
		Role:         role,
		Balance:      req.Balance,
		DailyLimit:   10_000_000,
		MonthlyLimit: 100_000_000,
	})
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	token, err := auth.CreateToken(s.cfg.JWTSecret, user.ID, user.Role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "user": user})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s Server) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	user, err := s.store.UserByEmail(r.Context(), req.Email)
	if err != nil || !auth.VerifyPassword(user.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if user.IsBlocked {
		writeError(w, http.StatusForbidden, "user is blocked")
		return
	}
	token, err := auth.CreateToken(s.cfg.JWTSecret, user.ID, user.Role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": user})
}

func (s Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentUser(r))
}

type createPaymentRequest struct {
	RecipientID int64  `json:"recipient_id"`
	Amount      int64  `json:"amount"`
	Description string `json:"description"`
	PaymentType string `json:"payment_type"`
}

func (s Server) createPayment(w http.ResponseWriter, r *http.Request) {
	var req createPaymentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.RecipientID == 0 || req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "recipient_id and positive amount are required")
		return
	}
	if req.PaymentType == "" {
		req.PaymentType = "SINGLE"
	}
	user := currentUser(r)
	payment, err := s.store.CreatePayment(r.Context(), store.Payment{
		SenderID:    user.ID,
		RecipientID: req.RecipientID,
		Amount:      req.Amount,
		PaymentType: req.PaymentType,
		Description: req.Description,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.publisher.PublishPayment(r.Context(), payment.ID); err != nil {
		writeError(w, http.StatusAccepted, "payment saved but queue publish failed")
		return
	}
	writeJSON(w, http.StatusCreated, payment)
}

func (s Server) listPayments(w http.ResponseWriter, r *http.Request) {
	payments, err := s.store.ListPayments(r.Context(), currentUser(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, payments)
}

func (s Server) getPayment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	payment, err := s.store.GetPayment(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, pgx.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	user := currentUser(r)
	if user.Role == store.RoleClient && payment.SenderID != user.ID && payment.RecipientID != user.ID {
		writeError(w, http.StatusForbidden, "payment is not available")
		return
	}
	writeJSON(w, http.StatusOK, payment)
}

func (s Server) bankerQueue(w http.ResponseWriter, r *http.Request) {
	payments, err := s.store.PendingPayments(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, payments)
}

func (s Server) searchClients(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.SearchUsers(r.Context(), store.UserSearch{
		Query: r.URL.Query().Get("q"),
		Role:  store.RoleClient,
		Limit: queryLimit(r, 100),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s Server) clientProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	profile, err := s.store.ClientProfile(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, pgx.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	if profile.User.Role != store.RoleClient {
		writeError(w, http.StatusBadRequest, "user is not a client")
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s Server) bankerStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.BankerStats(r.Context(), currentUser(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s Server) bankerHistory(w http.ResponseWriter, r *http.Request) {
	payments, err := s.store.DecisionsByBanker(r.Context(), currentUser(r).ID, queryLimit(r, 100))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, payments)
}

func (s Server) approvePayment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DecidePayment(r.Context(), id, currentUser(r).ID, store.StatusApproved, ""); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": store.StatusApproved})
}

type rejectRequest struct {
	Reason string `json:"reason"`
}

func (s Server) rejectPayment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req rejectRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := s.store.DecidePayment(r.Context(), id, currentUser(r).ID, store.StatusRejected, req.Reason); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": store.StatusRejected})
}

func (s Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	role := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("role")))
	if role != "" && role != store.RoleClient && role != store.RoleBanker && role != store.RoleAdmin {
		writeError(w, http.StatusBadRequest, "invalid role")
		return
	}
	users, err := s.store.SearchUsers(r.Context(), store.UserSearch{
		Query: r.URL.Query().Get("q"),
		Role:  role,
		Limit: queryLimit(r, 100),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s Server) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Email == "" || req.Password == "" || req.FullName == "" {
		writeError(w, http.StatusBadRequest, "email, password and full_name are required")
		return
	}
	role := req.Role
	if role == "" {
		role = store.RoleClient
	}
	if role != store.RoleClient && role != store.RoleBanker && role != store.RoleAdmin {
		writeError(w, http.StatusBadRequest, "invalid role")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}
	user, err := s.store.CreateUser(r.Context(), store.User{
		Email:        req.Email,
		PasswordHash: hash,
		FullName:     req.FullName,
		Phone:        req.Phone,
		Role:         role,
		Balance:      req.Balance,
		DailyLimit:   10_000_000,
		MonthlyLimit: 100_000_000,
	})
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (s Server) adminStats(w http.ResponseWriter, r *http.Request) {
	paymentStats, err := s.store.PaymentStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	bankerStats, err := s.store.AllBankerStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"payments": paymentStats,
		"bankers":  bankerStats,
	})
}

func (s Server) adminAudit(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.Audit(r.Context(), queryLimit(r, 200))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s Server) adminBankerHistory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	payments, err := s.store.DecisionsByBanker(r.Context(), id, queryLimit(r, 100))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, payments)
}

func (s Server) adminClientHistory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	profile, err := s.store.ClientProfile(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, pgx.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	audit, err := s.store.AuditForUser(r.Context(), id, queryLimit(r, 100))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"profile": profile,
		"audit":   audit,
	})
}

func (s Server) authenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		raw := strings.TrimPrefix(header, "Bearer ")
		if raw == header || raw == "" {
			writeError(w, http.StatusUnauthorized, "bearer token required")
			return
		}
		claims, err := auth.ParseToken(s.cfg.JWTSecret, raw)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		user, err := s.store.UserByID(r.Context(), claims.UserID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "user not found")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	})
}

func (s Server) requireRole(roles ...any) http.Handler {
	next := roles[len(roles)-1].(http.Handler)
	allowed := map[string]bool{}
	for _, role := range roles[:len(roles)-1] {
		allowed[role.(string)] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed[currentUser(r).Role] {
			writeError(w, http.StatusForbidden, "insufficient role")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func currentUser(r *http.Request) store.User {
	return r.Context().Value(userKey).(store.User)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func queryLimit(r *http.Request, fallback int) int {
	value := r.URL.Query().Get("limit")
	if value == "" {
		return fallback
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 {
		return fallback
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		next.ServeHTTP(w, r)
	})
}
