package payments

import "time"

type AuthorizationRequest struct {
	MerchantID     string  `json:"merchant"`
	UserID         string  `json:"user"`
	Amount         float64 `json:"amount"`
	Currency       string  `json:"currency"`
	PaymentMethod  string  `json:"payment_method"`
	DeviceID       string  `json:"device_id"`
	IP             string  `json:"ip"`
	Email          string  `json:"email"`
	Phone          string  `json:"phone"`
	BillingCity    string  `json:"billing_city"`
	BillingCountry string  `json:"billing_country"`
	CardHash       string  `json:"card_hash"`
}

type EvaluateRequest struct {
	PaymentID string               `json:"payment_id"`
	Payment   AuthorizationRequest `json:"payment"`
}

type FeatureSnapshot struct {
	Amount                 float64 `json:"amount"`
	UserAttempts5m         int64   `json:"user_attempts_5m"`
	DeviceAttempts5m       int64   `json:"device_attempts_5m"`
	DeviceUsers24h         int64   `json:"device_users_24h"`
	IPFailures1h           int64   `json:"ip_failures_1h"`
	MerchantRiskRatio15m   float64 `json:"merchant_risk_ratio_15m"`
	CardUsers24h           int64   `json:"card_users_24h"`
	BillingCountryMismatch bool    `json:"billing_country_mismatch"`
	IsHighAmount           bool    `json:"is_high_amount"`
	PaymentMethod          string  `json:"payment_method"`
	Currency               string  `json:"currency"`
}

type ModelScoreRequest struct {
	PaymentID string          `json:"payment_id"`
	Features  FeatureSnapshot `json:"features"`
}

type ModelScoreResponse struct {
	Score       int      `json:"score"`
	Label       string   `json:"label"`
	ReasonCodes []string `json:"reason_codes"`
}

type StoredDecision struct {
	PaymentID        string          `json:"payment_id"`
	Decision         string          `json:"decision"`
	RiskScore        int             `json:"risk_score"`
	ModelScore       int             `json:"model_score"`
	ModelLabel       string          `json:"model_label"`
	LatencyMS        int64           `json:"latency_ms"`
	TriggeredRules   []string        `json:"triggered_rules"`
	ModelReasonCodes []string        `json:"model_reason_codes"`
	FeatureSnapshot  FeatureSnapshot `json:"feature_snapshot"`
	CreatedAt        time.Time       `json:"created_at"`
}

type AuthorizationResponse struct {
	PaymentID      string   `json:"payment_id"`
	Decision       string   `json:"decision"`
	RiskScore      int      `json:"risk_score"`
	ModelScore     int      `json:"model_score"`
	LatencyMS      int64    `json:"latency_ms"`
	TriggeredRules []string `json:"triggered_rules"`
}

type PaymentRecord struct {
	ID             string    `json:"id"`
	MerchantID     string    `json:"merchant"`
	UserID         string    `json:"user"`
	Amount         float64   `json:"amount"`
	Currency       string    `json:"currency"`
	PaymentMethod  string    `json:"payment_method"`
	DeviceID       string    `json:"device_id"`
	IP             string    `json:"ip"`
	Email          string    `json:"email"`
	Phone          string    `json:"phone"`
	BillingCity    string    `json:"billing_city"`
	BillingCountry string    `json:"billing_country"`
	CardHash       string    `json:"card_hash"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

type PaymentView struct {
	Payment  PaymentRecord  `json:"payment"`
	Decision StoredDecision `json:"decision"`
}

type RequestedEvent struct {
	PaymentID string               `json:"payment_id"`
	Payment   AuthorizationRequest `json:"payment"`
}

type DecidedEvent struct {
	PaymentID string               `json:"payment_id"`
	Payment   AuthorizationRequest `json:"payment"`
	Decision  StoredDecision       `json:"decision"`
}
