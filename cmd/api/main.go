package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"bank-processing-data-subsystem/internal/config"
	"bank-processing-data-subsystem/internal/httpapi"
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

	publisher, err := queue.NewPublisher(cfg.RabbitMQURL)
	if err != nil {
		log.Fatal(err)
	}
	defer publisher.Close()

	server := httpapi.NewServer(cfg, db, publisher)
	httpServer := &http.Server{
		Addr:              ":" + cfg.APIPort,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("api listening on :%s", cfg.APIPort)
	log.Fatal(httpServer.ListenAndServe())
}
