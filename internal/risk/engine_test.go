package risk

import (
	"testing"

	"fraud-payments/internal/payments"
)

func TestDummyModelScoreHighRisk(t *testing.T) {
	features := payments.FeatureSnapshot{
		Amount:                 22000,
		UserAttempts5m:         5,
		DeviceAttempts5m:       4,
		DeviceUsers24h:         3,
		IPFailures1h:           4,
		MerchantRiskRatio15m:   0.50,
		CardUsers24h:           2,
		BillingCountryMismatch: true,
		IsHighAmount:           true,
	}

	result := DummyModelScore(features)
	if result.Score < 70 {
		t.Fatalf("expected high risk model score, got %d", result.Score)
	}
	if result.Label != "high" {
		t.Fatalf("expected high label, got %s", result.Label)
	}
}

func TestApplyRulesBlocksVerySharedDevice(t *testing.T) {
	features := payments.FeatureSnapshot{
		DeviceUsers24h: 4,
	}
	model := payments.ModelScoreResponse{
		Score:       45,
		Label:       "medium",
		ReasonCodes: []string{"shared_device"},
	}

	decision, score, rules := ApplyRules(features, model)
	if decision != "block" {
		t.Fatalf("expected block, got %s", decision)
	}
	if score < 80 {
		t.Fatalf("expected forced high score, got %d", score)
	}
	if len(rules) == 0 {
		t.Fatalf("expected rules to be present")
	}
}

func TestApplyRulesReviewBorderlineModel(t *testing.T) {
	features := payments.FeatureSnapshot{}
	model := payments.ModelScoreResponse{
		Score:       58,
		Label:       "medium",
		ReasonCodes: []string{"borderline_case"},
	}

	decision, score, _ := ApplyRules(features, model)
	if decision != "review" {
		t.Fatalf("expected review, got %s", decision)
	}
	if score != 60 {
		t.Fatalf("expected score bump to 60, got %d", score)
	}
}
