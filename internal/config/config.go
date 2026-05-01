package config

import (
	"crypto/tls"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	APIPort                string
	DatabaseURL            string
	RedisURL               string
	RabbitMQURL            string
	JWTSecret              string
	ProcessingServiceURL   string
	ProcessingLoginPath    string
	ProcessingProcessPath  string
	ProcessingAuthToken    string
	ProcessingAuthUsername string
	ProcessingAuthRole     string
	ProcessingTLSConfig    *tls.Config
	PaymentReviewThreshold int64
}

func Load() Config {
	skipProcessingTLSVerify := envBool("PROCESSING_INSECURE_SKIP_VERIFY", false)
	return Config{
		APIPort:                env("API_PORT", "8000"),
		DatabaseURL:            env("DATABASE_URL", "postgres://bank_user:bank_password@localhost:5432/bank_processing?sslmode=disable"),
		RedisURL:               env("REDIS_URL", "redis://localhost:6379/0"),
		RabbitMQURL:            env("RABBITMQ_URL", "amqp://bank_user:bank_password@localhost:5672/"),
		JWTSecret:              env("JWT_SECRET", "change-me-in-production-32-chars-min"),
		ProcessingServiceURL:   env("PROCESSING_SERVICE_URL", "https://pay.projectl.ru"),
		ProcessingLoginPath:    env("PROCESSING_LOGIN_PATH", "/api/v1/auth/login"),
		ProcessingProcessPath:  env("PROCESSING_PROCESS_PATH", "/api/v1/payments/process"),
		ProcessingAuthToken:    env("PROCESSING_AUTH_TOKEN", ""),
		ProcessingAuthUsername: env("PROCESSING_AUTH_USERNAME", "tester"),
		ProcessingAuthRole:     env("PROCESSING_AUTH_ROLE", "USER"),
		ProcessingTLSConfig:    tlsConfig(skipProcessingTLSVerify),
		PaymentReviewThreshold: envInt64("PAYMENT_REVIEW_THRESHOLD", 100000),
	}
}

func (c Config) RedisAddr() string {
	parsed, err := url.Parse(c.RedisURL)
	if err != nil || parsed.Host == "" {
		return strings.TrimPrefix(c.RedisURL, "redis://")
	}
	return parsed.Host
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envInt64(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func tlsConfig(skipVerify bool) *tls.Config {
	if !skipVerify {
		return nil
	}
	return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // Local integration flag for externally misconfigured TLS.
}
