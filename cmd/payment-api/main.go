package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"fraud-payments/internal/config"
	"fraud-payments/internal/events"
	"fraud-payments/internal/payments"
	"fraud-payments/internal/risk"
	"fraud-payments/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	databaseURL := config.String("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/fraud?sslmode=disable")
	redisAddr := config.String("REDIS_ADDR", "localhost:6379")
	riskEngineURL := config.String("RISK_ENGINE_URL", "http://localhost:8081")
	migrationsDir := config.String("MIGRATIONS_DIR", "migrations")
	kafkaBrokers := config.CSV("KAFKA_BROKERS", []string{"localhost:9092"})
	port := config.Int("APP_PORT", 8080)

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

	publisher := events.NewPublisher(kafkaBrokers)
	defer publisher.Close()

	riskClient := risk.NewClient(riskEngineURL)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/v1/payments/authorize", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}

		fmt.Println("payment-api authorize started")

		var payload payments.AuthorizationRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := validateRequest(payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		fmt.Println("payment-api request decoded")

		paymentID := config.NewID("pay")
		if err := dataStore.SavePayment(r.Context(), paymentID, payload); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		fmt.Println("payment-api payment stored")

		decision, err := riskClient.Evaluate(r.Context(), payments.EvaluateRequest{
			PaymentID: paymentID,
			Payment:   payload,
		})
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}

		fmt.Println("payment-api risk result received")

		if err := dataStore.SaveDecision(r.Context(), decision); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		fmt.Println("payment-api decision stored")

		if err := publisher.PublishPaymentRequested(r.Context(), paymentID, payload); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		if err := publisher.PublishPaymentDecided(r.Context(), paymentID, payload, decision); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}

		fmt.Println("payment-api events published")

		writeJSON(w, http.StatusOK, payments.AuthorizationResponse{
			PaymentID:      paymentID,
			Decision:       decision.Decision,
			RiskScore:      decision.RiskScore,
			ModelScore:     decision.ModelScore,
			LatencyMS:      decision.LatencyMS,
			TriggeredRules: decision.TriggeredRules,
		})
	})
	mux.HandleFunc("/v1/payments/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}

		paymentID := strings.TrimPrefix(r.URL.Path, "/v1/payments/")
		view, err := dataStore.GetPaymentView(r.Context(), paymentID)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "payment not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, view)
	})
	mux.HandleFunc("/v1/decisions/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}

		paymentID := strings.TrimPrefix(r.URL.Path, "/v1/decisions/")
		decision, err := dataStore.GetDecision(r.Context(), paymentID)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "decision not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, decision)
	})
	mux.HandleFunc("/v1/demo/risky-payments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		items, err := dataStore.ListRiskyPayments(r.Context(), 10)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	})
	mux.HandleFunc("/v1/demo/seed", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}

		fmt.Println("payment-api demo seeding started")

		if err := dataStore.SeedDemo(r.Context()); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		fmt.Println("payment-api demo seeding finished")

		writeJSON(w, http.StatusOK, map[string]string{"status": "seeded"})
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

	log.Printf("payment-api listening on :%d", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func validateRequest(req payments.AuthorizationRequest) error {
	if req.MerchantID == "" {
		return errors.New("merchant is required")
	}
	if req.UserID == "" {
		return errors.New("user is required")
	}
	if req.Amount <= 0 {
		return errors.New("amount must be greater than zero")
	}
	if req.Currency == "" {
		return errors.New("currency is required")
	}
	if req.PaymentMethod == "" {
		return errors.New("payment_method is required")
	}
	if req.DeviceID == "" {
		return errors.New("device_id is required")
	}
	if req.IP == "" {
		return errors.New("ip is required")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
