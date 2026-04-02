# Fraud-payments

Backend-only fraud detection prototype in Go. It simulates inline risk scoring inside the payment flow with a payment API, a risk engine, a dummy model service, and a stream worker that updates counters after each transaction.

## Stack

- Go services
- Postgres
- Redis
- Redpanda
- Docker Compose

## Services

- `payment-api`: public demo API
- `risk-engine`: feature gathering and rule layer
- `model-service`: dummy inference endpoint
- `stream-worker`: async feature updates
- `demo-seeder`: loads sample demo data

## Run

```bash
docker-compose -f deployments/docker-compose.yml up --build
```

In a second terminal:

```bash
chmod +x scripts/demo.sh
./scripts/demo.sh
```

## Main API

### Authorize payment

```bash
curl -sS -X POST http://localhost:8080/v1/payments/authorize \
  -H "Content-Type: application/json" \
  -d '{
    "merchant": "m-clean",
    "user": "user-1",
    "amount": 1500,
    "currency": "INR",
    "payment_method": "card",
    "device_id": "device-1",
    "ip": "198.51.100.10",
    "email": "user@test.local",
    "phone": "9000000000",
    "billing_city": "Bengaluru",
    "billing_country": "IN",
    "card_hash": "card-1"
  }'
```

### Seed demo data

```bash
curl -sS -X POST http://localhost:8080/v1/demo/seed
```

### View risky decisions

```bash
curl -sS http://localhost:8080/v1/demo/risky-payments
```
