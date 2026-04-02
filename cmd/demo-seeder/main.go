package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"fraud-payments/internal/config"
	"fraud-payments/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	databaseURL := config.String("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/fraud?sslmode=disable")
	redisAddr := config.String("REDIS_ADDR", "localhost:6379")
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

	fmt.Println("demo-seeder started")

	if err := dataStore.SeedDemo(ctx); err != nil {
		log.Fatal(err)
	}

	fmt.Println("demo-seeder finished")
}
