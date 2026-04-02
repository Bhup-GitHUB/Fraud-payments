#!/usr/bin/env bash

set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"

echo "seeding demo data"
curl -sS -X POST "$BASE_URL/v1/demo/seed"
echo
echo

echo "sending clean payment"
curl -sS -X POST "$BASE_URL/v1/payments/authorize" \
  -H "Content-Type: application/json" \
  -d '{
    "merchant": "m-clean",
    "user": "user-clean",
    "amount": 1200,
    "currency": "INR",
    "payment_method": "card",
    "device_id": "device-clean",
    "ip": "198.51.100.20",
    "email": "clean@test.local",
    "phone": "9000000111",
    "billing_city": "Pune",
    "billing_country": "IN",
    "card_hash": "card-clean"
  }'
echo
echo

echo "sending suspicious payment"
curl -sS -X POST "$BASE_URL/v1/payments/authorize" \
  -H "Content-Type: application/json" \
  -d '{
    "merchant": "m-risk",
    "user": "user-retry",
    "amount": 22000,
    "currency": "INR",
    "payment_method": "card",
    "device_id": "shared-device",
    "ip": "203.0.113.9",
    "email": "suspicious@test.local",
    "phone": "9000000222",
    "billing_city": "Dubai",
    "billing_country": "AE",
    "card_hash": "card-alpha"
  }'
echo
echo

echo "recent risky payments"
curl -sS "$BASE_URL/v1/demo/risky-payments"
echo
