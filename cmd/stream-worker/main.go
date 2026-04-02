package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fraud-payments/internal/config"
	"fraud-payments/internal/events"
	"fraud-payments/internal/payments"
	"fraud-payments/internal/store"

	"github.com/segmentio/kafka-go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	databaseURL := config.String("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/fraud?sslmode=disable")
	redisAddr := config.String("REDIS_ADDR", "localhost:6379")
	kafkaBrokers := config.CSV("KAFKA_BROKERS", []string{"localhost:9092"})
	migrationsDir := config.String("MIGRATIONS_DIR", "migrations")

	db, err := store.OpenPostgres(ctx, databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	redisClient, err := store.OpenRedis(ctx, redisAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer redisClient.Close()

	dataStore := store.New(db, redisClient)
	if err := dataStore.EnsureSchema(ctx, migrationsDir); err != nil {
		log.Fatal(err)
	}

	requestedReader := events.NewReader(kafkaBrokers, events.TopicPaymentRequested, "fraud-stream-worker")
	decidedReader := events.NewReader(kafkaBrokers, events.TopicPaymentDecided, "fraud-stream-worker")
	defer requestedReader.Close()
	defer decidedReader.Close()

	errCh := make(chan error, 2)

	go func() {
		errCh <- consumeRequested(ctx, requestedReader, dataStore)
	}()
	go func() {
		errCh <- consumeDecided(ctx, decidedReader, dataStore)
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil {
			log.Fatal(err)
		}
	}
}

func consumeRequested(ctx context.Context, reader *kafka.Reader, dataStore *store.Store) error {
	for {
		message, err := reader.FetchMessage(ctx)
		if err != nil {
			return err
		}
		envelope, err := events.DecodeEnvelope(message.Value)
		if err != nil {
			return err
		}
		var payload payments.RequestedEvent
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return err
		}
		if err := dataStore.TrackPaymentRequested(ctx, payload); err != nil {
			return err
		}
		if err := reader.CommitMessages(ctx, message); err != nil {
			return err
		}
	}
}

func consumeDecided(ctx context.Context, reader *kafka.Reader, dataStore *store.Store) error {
	for {
		message, err := reader.FetchMessage(ctx)
		if err != nil {
			return err
		}
		envelope, err := events.DecodeEnvelope(message.Value)
		if err != nil {
			return err
		}
		var payload payments.DecidedEvent
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return err
		}
		if err := dataStore.TrackPaymentDecided(ctx, payload); err != nil {
			return err
		}
		if err := reader.CommitMessages(ctx, message); err != nil {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
}
