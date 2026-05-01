package main

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"

	"bank-processing-data-subsystem/internal/config"
	"bank-processing-data-subsystem/internal/processing"
	"bank-processing-data-subsystem/internal/queue"
	"bank-processing-data-subsystem/internal/store"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		log.Fatal(err)
	}

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr()})
	defer redisClient.Close()

	consumer, err := queue.NewConsumer(cfg.RabbitMQURL)
	if err != nil {
		log.Fatal(err)
	}
	defer consumer.Close()

	processor := processing.NewClient(processing.Config{
		BaseURL:      cfg.ProcessingServiceURL,
		LoginPath:    cfg.ProcessingLoginPath,
		ProcessPath:  cfg.ProcessingProcessPath,
		AuthToken:    cfg.ProcessingAuthToken,
		AuthUsername: cfg.ProcessingAuthUsername,
		AuthRole:     cfg.ProcessingAuthRole,
		TLSConfig:    cfg.ProcessingTLSConfig,
	})
	log.Println("worker waiting for payment jobs")

	for {
		if err := consumer.ConsumePayments(ctx, func(ctx context.Context, job queue.PaymentJob) error {
			payment, err := db.GetPayment(ctx, job.PaymentID)
			if err != nil {
				log.Printf("failed to load payment %d: %v", job.PaymentID, err)
				return err
			}

			result, err := processor.Score(ctx, payment)
			if err != nil {
				log.Printf("processing payment %d failed: %v", payment.ID, err)
				return err
			}

			if err := db.ApplyProcessingResult(ctx, payment.ID, result.Status, result.FraudScore, result.Reason); err != nil {
				log.Printf("failed to save processing result for payment %d: %v", payment.ID, err)
				return err
			}

			if err := redisClient.Del(ctx, "banker:queue").Err(); err != nil {
				log.Printf("redis cache invalidation failed: %v", err)
			}
			return nil
		}); err != nil {
			log.Printf("consume failed: %v", err)
			time.Sleep(3 * time.Second)
		}
	}
}
