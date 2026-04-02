package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fraud-payments/internal/config"
	"fraud-payments/internal/features"
	"fraud-payments/internal/modelclient"
	"fraud-payments/internal/payments"
	"fraud-payments/internal/risk"
	"fraud-payments/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	databaseURL := config.String("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/fraud?sslmode=disable")
	redisAddr := config.String("REDIS_ADDR", "localhost:6379")
	modelServiceURL := config.String("MODEL_SERVICE_URL", "http://localhost:8082")
	migrationsDir := config.String("MIGRATIONS_DIR", "migrations")
	port := config.Int("APP_PORT", 8081)

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

	engine := &risk.Engine{
		Builder: &features.Builder{Store: dataStore},
		Model:   modelclient.New(modelServiceURL),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/internal/risk/evaluate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}

		fmt.Println("risk-engine request started")

		var payload payments.EvaluateRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		fmt.Println("risk-engine payload decoded")

		decision, err := engine.Evaluate(r.Context(), payload)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}

		fmt.Println("risk-engine decision ready")

		writeJSON(w, http.StatusOK, decision)
	})

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("risk-engine listening on :%d", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
