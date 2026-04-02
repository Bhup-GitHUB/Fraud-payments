package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"fraud-payments/internal/config"
	"fraud-payments/internal/payments"
	"fraud-payments/internal/risk"
)

func main() {
	port := config.Int("APP_PORT", 8082)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/internal/model/score", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}

		fmt.Println("model-service request started")

		var payload payments.ModelScoreRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		fmt.Println("model-service features decoded")

		score := risk.DummyModelScore(payload.Features)

		fmt.Println("model-service score generated")

		writeJSON(w, http.StatusOK, score)
	})

	log.Printf("model-service listening on :%d", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), mux); err != nil {
		log.Fatal(err)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
