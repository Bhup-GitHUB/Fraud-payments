package risk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"fraud-payments/internal/features"
	"fraud-payments/internal/modelclient"
	"fraud-payments/internal/payments"
)

type Engine struct {
	Builder *features.Builder
	Model   *modelclient.Client
}

func (e *Engine) Evaluate(ctx context.Context, req payments.EvaluateRequest) (payments.StoredDecision, error) {
	started := time.Now()
	featureSnapshot, err := e.Builder.Build(ctx, req.Payment)
	if err != nil {
		return payments.StoredDecision{}, err
	}
	modelResponse, err := e.Model.Score(ctx, req.PaymentID, featureSnapshot)
	if err != nil {
		return payments.StoredDecision{}, err
	}
	decision, riskScore, triggered := ApplyRules(featureSnapshot, modelResponse)
	return payments.StoredDecision{
		PaymentID:        req.PaymentID,
		Decision:         decision,
		RiskScore:        riskScore,
		ModelScore:       modelResponse.Score,
		ModelLabel:       modelResponse.Label,
		LatencyMS:        time.Since(started).Milliseconds(),
		TriggeredRules:   triggered,
		ModelReasonCodes: modelResponse.ReasonCodes,
		FeatureSnapshot:  featureSnapshot,
		CreatedAt:        time.Now().UTC(),
	}, nil
}

func DummyModelScore(features payments.FeatureSnapshot) payments.ModelScoreResponse {
	score := 8
	reasons := make([]string, 0)

	if features.IsHighAmount {
		score += 12
		reasons = append(reasons, "high_amount")
	}
	if features.UserAttempts5m >= 3 {
		score += 18
		reasons = append(reasons, "user_retry_burst")
	}
	if features.UserAttempts5m >= 5 {
		score += 8
		reasons = append(reasons, "heavy_user_retry_burst")
	}
	if features.DeviceAttempts5m >= 3 {
		score += 10
		reasons = append(reasons, "device_retry_burst")
	}
	if features.DeviceUsers24h >= 2 {
		score += 22
		reasons = append(reasons, "shared_device")
	}
	if features.IPFailures1h >= 3 {
		score += 16
		reasons = append(reasons, "ip_failure_velocity")
	}
	if features.MerchantRiskRatio15m >= 0.30 {
		score += 14
		reasons = append(reasons, "merchant_risk_cluster")
	}
	if features.CardUsers24h >= 2 {
		score += 18
		reasons = append(reasons, "card_shared_across_users")
	}
	if features.BillingCountryMismatch {
		score += 10
		reasons = append(reasons, "billing_country_mismatch")
	}

	if score > 95 {
		score = 95
	}
	return payments.ModelScoreResponse{
		Score:       score,
		Label:       labelForScore(score),
		ReasonCodes: uniqueStrings(reasons),
	}
}

func ApplyRules(features payments.FeatureSnapshot, model payments.ModelScoreResponse) (string, int, []string) {
	score := model.Score
	rules := append([]string{}, model.ReasonCodes...)
	decision := decisionForScore(score)

	if features.DeviceUsers24h >= 4 {
		if score < 84 {
			score = 84
		}
		decision = "block"
		rules = append(rules, "shared_device_block")
	}

	if features.IPFailures1h >= 5 && features.UserAttempts5m >= 3 {
		if score < 82 {
			score = 82
		}
		decision = "block"
		rules = append(rules, "retry_burst_block")
	}

	if score >= 55 && score < 70 {
		if score < 60 {
			score = 60
		}
		if decision == "approve" {
			decision = "review"
		}
		rules = append(rules, "borderline_model_review")
	}

	if decision == "approve" && score >= 40 {
		decision = "review"
	}

	return decision, score, uniqueStrings(rules)
}

func labelForScore(score int) string {
	if score >= 70 {
		return "high"
	}
	if score >= 40 {
		return "medium"
	}
	return "low"
}

func decisionForScore(score int) string {
	if score >= 70 {
		return "block"
	}
	if score >= 40 {
		return "review"
	}
	return "approve"
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

func (c *Client) Evaluate(ctx context.Context, req payments.EvaluateRequest) (payments.StoredDecision, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return payments.StoredDecision{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/internal/risk/evaluate", bytes.NewReader(body))
	if err != nil {
		return payments.StoredDecision{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := c.HTTPClient.Do(httpRequest)
	if err != nil {
		return payments.StoredDecision{}, err
	}
	defer response.Body.Close()

	if response.StatusCode >= 300 {
		return payments.StoredDecision{}, fmt.Errorf("risk engine returned %s", response.Status)
	}
	var payload payments.StoredDecision
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return payments.StoredDecision{}, err
	}
	return payload, nil
}
