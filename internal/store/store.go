package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"fraud-payments/internal/config"
	"fraud-payments/internal/payments"

	"github.com/redis/go-redis/v9"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Store struct {
	DB    *sql.DB
	Redis *redis.Client
}

func OpenPostgres(ctx context.Context, databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	return db, nil
}

func OpenRedis(ctx context.Context, address string) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{Addr: address})
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return client, nil
}

func New(db *sql.DB, redisClient *redis.Client) *Store {
	return &Store{DB: db, Redis: redisClient}
}

func (s *Store) EnsureSchema(ctx context.Context, migrationsDir string) error {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		files = append(files, filepath.Join(migrationsDir, entry.Name()))
	}
	sort.Strings(files)
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if _, err := s.DB.ExecContext(ctx, string(body)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SavePayment(ctx context.Context, paymentID string, req payments.AuthorizationRequest) error {
	_, err := s.DB.ExecContext(ctx, `
		insert into payments (
			id, merchant_id, user_id, amount, currency, payment_method, device_id, ip,
			email, phone, billing_city, billing_country, card_hash, status
		) values (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13, $14
		)
	`, paymentID, req.MerchantID, req.UserID, req.Amount, req.Currency, req.PaymentMethod, req.DeviceID, req.IP, req.Email, req.Phone, req.BillingCity, req.BillingCountry, req.CardHash, "received")
	return err
}

func (s *Store) SaveDecision(ctx context.Context, decision payments.StoredDecision) error {
	triggeredRules, err := json.Marshal(decision.TriggeredRules)
	if err != nil {
		return err
	}
	modelReasons, err := json.Marshal(decision.ModelReasonCodes)
	if err != nil {
		return err
	}
	features, err := json.Marshal(decision.FeatureSnapshot)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		insert into fraud_decisions (
			payment_id, decision, risk_score, model_score, model_label,
			triggered_rules, model_reason_codes, feature_snapshot, latency_ms
		) values (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9
		)
		on conflict (payment_id) do update set
			decision = excluded.decision,
			risk_score = excluded.risk_score,
			model_score = excluded.model_score,
			model_label = excluded.model_label,
			triggered_rules = excluded.triggered_rules,
			model_reason_codes = excluded.model_reason_codes,
			feature_snapshot = excluded.feature_snapshot,
			latency_ms = excluded.latency_ms
	`, decision.PaymentID, decision.Decision, decision.RiskScore, decision.ModelScore, decision.ModelLabel, triggeredRules, modelReasons, features, decision.LatencyMS); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `update payments set status = $2 where id = $1`, decision.PaymentID, decision.Decision); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetPayment(ctx context.Context, paymentID string) (payments.PaymentRecord, error) {
	var record payments.PaymentRecord
	err := s.DB.QueryRowContext(ctx, `
		select id, merchant_id, user_id, amount, currency, payment_method, device_id, ip,
		       email, phone, billing_city, billing_country, card_hash, status, created_at
		from payments
		where id = $1
	`, paymentID).Scan(
		&record.ID, &record.MerchantID, &record.UserID, &record.Amount, &record.Currency,
		&record.PaymentMethod, &record.DeviceID, &record.IP, &record.Email, &record.Phone,
		&record.BillingCity, &record.BillingCountry, &record.CardHash, &record.Status, &record.CreatedAt,
	)
	return record, err
}

func (s *Store) GetDecision(ctx context.Context, paymentID string) (payments.StoredDecision, error) {
	var decision payments.StoredDecision
	var triggeredRules []byte
	var modelReasons []byte
	var features []byte
	err := s.DB.QueryRowContext(ctx, `
		select payment_id, decision, risk_score, model_score, model_label,
		       triggered_rules, model_reason_codes, feature_snapshot, latency_ms, created_at
		from fraud_decisions
		where payment_id = $1
	`, paymentID).Scan(
		&decision.PaymentID, &decision.Decision, &decision.RiskScore, &decision.ModelScore,
		&decision.ModelLabel, &triggeredRules, &modelReasons, &features, &decision.LatencyMS, &decision.CreatedAt,
	)
	if err != nil {
		return decision, err
	}
	_ = json.Unmarshal(triggeredRules, &decision.TriggeredRules)
	_ = json.Unmarshal(modelReasons, &decision.ModelReasonCodes)
	_ = json.Unmarshal(features, &decision.FeatureSnapshot)
	return decision, nil
}

func (s *Store) GetPaymentView(ctx context.Context, paymentID string) (payments.PaymentView, error) {
	record, err := s.GetPayment(ctx, paymentID)
	if err != nil {
		return payments.PaymentView{}, err
	}
	decision, err := s.GetDecision(ctx, paymentID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return payments.PaymentView{}, err
	}
	return payments.PaymentView{Payment: record, Decision: decision}, nil
}

func (s *Store) ListRiskyPayments(ctx context.Context, limit int) ([]payments.PaymentView, error) {
	rows, err := s.DB.QueryContext(ctx, `
		select p.id
		from payments p
		join fraud_decisions d on d.payment_id = p.id
		where d.decision in ('review', 'block')
		order by p.created_at desc
		limit $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]payments.PaymentView, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		item, err := s.GetPaymentView(ctx, id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) MerchantHomeCountry(ctx context.Context, merchantID string) (string, error) {
	var country string
	err := s.DB.QueryRowContext(ctx, `select home_country from merchants where id = $1`, merchantID).Scan(&country)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return country, err
}

func (s *Store) countDistinct(ctx context.Context, query string, value string) (int64, error) {
	var count int64
	err := s.DB.QueryRowContext(ctx, query, value, time.Now().Add(-24*time.Hour)).Scan(&count)
	return count, err
}

func (s *Store) DeviceUsers24h(ctx context.Context, deviceID string) (int64, error) {
	return s.countDistinct(ctx, `select count(distinct user_id) from device_links where device_id = $1 and seen_at >= $2`, deviceID)
}

func (s *Store) CardUsers24h(ctx context.Context, cardHash string) (int64, error) {
	if cardHash == "" {
		return 0, nil
	}
	return s.countDistinct(ctx, `select count(distinct user_id) from card_links where card_hash = $1 and seen_at >= $2`, cardHash)
}

func (s *Store) ReadFeatureSnapshot(ctx context.Context, req payments.AuthorizationRequest) (payments.FeatureSnapshot, error) {
	userAttempts, err := s.getInt(ctx, velocityUserKey(req.UserID))
	if err != nil {
		return payments.FeatureSnapshot{}, err
	}
	deviceAttempts, err := s.getInt(ctx, velocityDeviceKey(req.DeviceID))
	if err != nil {
		return payments.FeatureSnapshot{}, err
	}
	ipFailures, err := s.getInt(ctx, ipFailuresKey(req.IP))
	if err != nil {
		return payments.FeatureSnapshot{}, err
	}
	merchantTotal, err := s.getInt(ctx, merchantTotalKey(req.MerchantID))
	if err != nil {
		return payments.FeatureSnapshot{}, err
	}
	merchantRisky, err := s.getInt(ctx, merchantRiskyKey(req.MerchantID))
	if err != nil {
		return payments.FeatureSnapshot{}, err
	}
	deviceUsers, err := s.DeviceUsers24h(ctx, req.DeviceID)
	if err != nil {
		return payments.FeatureSnapshot{}, err
	}
	cardUsers, err := s.CardUsers24h(ctx, req.CardHash)
	if err != nil {
		return payments.FeatureSnapshot{}, err
	}
	merchantCountry, err := s.MerchantHomeCountry(ctx, req.MerchantID)
	if err != nil {
		return payments.FeatureSnapshot{}, err
	}
	ratio := 0.0
	if merchantTotal > 0 {
		ratio = float64(merchantRisky) / float64(merchantTotal)
	}
	mismatch := false
	if merchantCountry != "" && req.BillingCountry != "" && merchantCountry != req.BillingCountry {
		mismatch = true
	}
	return payments.FeatureSnapshot{
		Amount:                 req.Amount,
		UserAttempts5m:         userAttempts,
		DeviceAttempts5m:       deviceAttempts,
		DeviceUsers24h:         deviceUsers,
		IPFailures1h:           ipFailures,
		MerchantRiskRatio15m:   ratio,
		CardUsers24h:           cardUsers,
		BillingCountryMismatch: mismatch,
		IsHighAmount:           req.Amount >= 15000,
		PaymentMethod:          req.PaymentMethod,
		Currency:               req.Currency,
	}, nil
}

func (s *Store) TrackPaymentRequested(ctx context.Context, event payments.RequestedEvent) error {
	if err := s.UpsertLinks(ctx, event.Payment); err != nil {
		return err
	}
	pipe := s.Redis.TxPipeline()
	pipe.Incr(ctx, velocityUserKey(event.Payment.UserID))
	pipe.Expire(ctx, velocityUserKey(event.Payment.UserID), 5*time.Minute)
	pipe.Incr(ctx, velocityDeviceKey(event.Payment.DeviceID))
	pipe.Expire(ctx, velocityDeviceKey(event.Payment.DeviceID), 5*time.Minute)
	pipe.Incr(ctx, merchantTotalKey(event.Payment.MerchantID))
	pipe.Expire(ctx, merchantTotalKey(event.Payment.MerchantID), 15*time.Minute)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *Store) TrackPaymentDecided(ctx context.Context, event payments.DecidedEvent) error {
	if event.Decision.Decision == "approve" {
		return nil
	}
	pipe := s.Redis.TxPipeline()
	pipe.Incr(ctx, ipFailuresKey(event.Payment.IP))
	pipe.Expire(ctx, ipFailuresKey(event.Payment.IP), time.Hour)
	pipe.Incr(ctx, merchantRiskyKey(event.Payment.MerchantID))
	pipe.Expire(ctx, merchantRiskyKey(event.Payment.MerchantID), 15*time.Minute)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *Store) UpsertLinks(ctx context.Context, req payments.AuthorizationRequest) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		insert into device_links (device_id, user_id, seen_at)
		values ($1, $2, now())
		on conflict (device_id, user_id) do update set seen_at = excluded.seen_at
	`, req.DeviceID, req.UserID); err != nil {
		return err
	}
	if req.CardHash != "" {
		if _, err := tx.ExecContext(ctx, `
			insert into card_links (card_hash, user_id, seen_at)
			values ($1, $2, now())
			on conflict (card_hash, user_id) do update set seen_at = excluded.seen_at
		`, req.CardHash, req.UserID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		insert into ip_links (ip, user_id, seen_at)
		values ($1, $2, now())
		on conflict (ip, user_id) do update set seen_at = excluded.seen_at
	`, req.IP, req.UserID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SeedDemo(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, `
		truncate fraud_decisions, payments, device_links, card_links, ip_links, merchants restart identity cascade
	`); err != nil {
		return err
	}
	if s.Redis != nil {
		if err := s.Redis.FlushDB(ctx).Err(); err != nil {
			return err
		}
	}
	merchants := []struct {
		ID          string
		Name        string
		HomeCountry string
	}{
		{ID: "m-clean", Name: "Calm Store", HomeCountry: "IN"},
		{ID: "m-risk", Name: "Risky Gadgets", HomeCountry: "IN"},
	}
	for _, merchant := range merchants {
		if _, err := s.DB.ExecContext(ctx, `
			insert into merchants (id, name, home_country)
			values ($1, $2, $3)
			on conflict (id) do update set name = excluded.name, home_country = excluded.home_country
		`, merchant.ID, merchant.Name, merchant.HomeCountry); err != nil {
			return err
		}
	}

	base := []payments.AuthorizationRequest{
		{
			MerchantID:     "m-risk",
			UserID:         "user-a",
			Amount:         1999,
			Currency:       "INR",
			PaymentMethod:  "card",
			DeviceID:       "shared-device",
			IP:             "10.10.1.2",
			Email:          "user-a@test.local",
			Phone:          "9000000001",
			BillingCity:    "Bengaluru",
			BillingCountry: "IN",
			CardHash:       "card-alpha",
		},
		{
			MerchantID:     "m-risk",
			UserID:         "user-b",
			Amount:         2400,
			Currency:       "INR",
			PaymentMethod:  "card",
			DeviceID:       "shared-device",
			IP:             "10.10.1.3",
			Email:          "user-b@test.local",
			Phone:          "9000000002",
			BillingCity:    "Mumbai",
			BillingCountry: "IN",
			CardHash:       "card-alpha",
		},
		{
			MerchantID:     "m-risk",
			UserID:         "user-c",
			Amount:         3200,
			Currency:       "INR",
			PaymentMethod:  "upi",
			DeviceID:       "shared-device",
			IP:             "10.10.1.4",
			Email:          "user-c@test.local",
			Phone:          "9000000003",
			BillingCity:    "Delhi",
			BillingCountry: "IN",
			CardHash:       "card-beta",
		},
	}

	for index, req := range base {
		paymentID := config.NewID("seedpay")
		if err := s.SavePayment(ctx, paymentID, req); err != nil {
			return err
		}
		decision := payments.StoredDecision{
			PaymentID:        paymentID,
			Decision:         []string{"review", "block", "block"}[index],
			RiskScore:        []int{58, 81, 88}[index],
			ModelScore:       []int{52, 77, 84}[index],
			ModelLabel:       []string{"medium", "high", "high"}[index],
			LatencyMS:        21,
			TriggeredRules:   []string{"seeded_case"},
			ModelReasonCodes: []string{"seed_profile"},
			FeatureSnapshot: payments.FeatureSnapshot{
				Amount:                 req.Amount,
				UserAttempts5m:         2,
				DeviceAttempts5m:       4,
				DeviceUsers24h:         3,
				IPFailures1h:           2,
				MerchantRiskRatio15m:   0.4,
				CardUsers24h:           2,
				BillingCountryMismatch: false,
				IsHighAmount:           false,
				PaymentMethod:          req.PaymentMethod,
				Currency:               req.Currency,
			},
			CreatedAt: time.Now(),
		}
		if err := s.SaveDecision(ctx, decision); err != nil {
			return err
		}
		if err := s.UpsertLinks(ctx, req); err != nil {
			return err
		}
	}

	if err := s.setInt(ctx, velocityUserKey("user-retry"), 4, 5*time.Minute); err != nil {
		return err
	}
	if err := s.setInt(ctx, velocityDeviceKey("shared-device"), 3, 5*time.Minute); err != nil {
		return err
	}
	if err := s.setInt(ctx, ipFailuresKey("203.0.113.9"), 5, time.Hour); err != nil {
		return err
	}
	if err := s.setInt(ctx, merchantTotalKey("m-risk"), 8, 15*time.Minute); err != nil {
		return err
	}
	if err := s.setInt(ctx, merchantRiskyKey("m-risk"), 4, 15*time.Minute); err != nil {
		return err
	}
	return nil
}

func (s *Store) getInt(ctx context.Context, key string) (int64, error) {
	value, err := s.Redis.Get(ctx, key).Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

func (s *Store) setInt(ctx context.Context, key string, value int64, ttl time.Duration) error {
	return s.Redis.Set(ctx, key, value, ttl).Err()
}

func velocityUserKey(userID string) string {
	return "velocity:user:" + userID + ":5m"
}

func velocityDeviceKey(deviceID string) string {
	return "velocity:device:" + deviceID + ":5m"
}

func ipFailuresKey(ip string) string {
	return "velocity:ip:" + ip + ":1h:failures"
}

func merchantTotalKey(merchantID string) string {
	return "merchant:" + merchantID + ":15m:total"
}

func merchantRiskyKey(merchantID string) string {
	return "merchant:" + merchantID + ":15m:risky"
}
