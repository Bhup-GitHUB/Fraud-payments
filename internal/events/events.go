package events

import (
	"context"
	"encoding/json"
	"time"

	"fraud-payments/internal/config"
	"fraud-payments/internal/payments"

	"github.com/segmentio/kafka-go"
)

const (
	TopicPaymentRequested = "payment.requested"
	TopicPaymentDecided   = "payment.decided"
)

type Envelope struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	PaymentID string          `json:"payment_id"`
	CreatedAt time.Time       `json:"created_at"`
	Payload   json.RawMessage `json:"payload"`
}

type Publisher struct {
	writer *kafka.Writer
}

func NewPublisher(brokers []string) *Publisher {
	return &Publisher{
		writer: &kafka.Writer{
			Addr:         kafka.TCP(brokers...),
			Balancer:     &kafka.LeastBytes{},
			RequiredAcks: kafka.RequireOne,
		},
	}
}

func (p *Publisher) Close() error {
	return p.writer.Close()
}

func (p *Publisher) PublishPaymentRequested(ctx context.Context, paymentID string, req payments.AuthorizationRequest) error {
	payload, err := json.Marshal(payments.RequestedEvent{PaymentID: paymentID, Payment: req})
	if err != nil {
		return err
	}
	return p.publish(ctx, TopicPaymentRequested, paymentID, payload)
}

func (p *Publisher) PublishPaymentDecided(ctx context.Context, paymentID string, req payments.AuthorizationRequest, decision payments.StoredDecision) error {
	payload, err := json.Marshal(payments.DecidedEvent{PaymentID: paymentID, Payment: req, Decision: decision})
	if err != nil {
		return err
	}
	return p.publish(ctx, TopicPaymentDecided, paymentID, payload)
}

func (p *Publisher) publish(ctx context.Context, topic, paymentID string, payload []byte) error {
	envelope, err := json.Marshal(Envelope{
		ID:        config.NewID("evt"),
		Type:      topic,
		PaymentID: paymentID,
		CreatedAt: time.Now().UTC(),
		Payload:   payload,
	})
	if err != nil {
		return err
	}
	return p.writer.WriteMessages(ctx, kafka.Message{
		Topic: topic,
		Key:   []byte(paymentID),
		Value: envelope,
		Time:  time.Now().UTC(),
	})
}

func NewReader(brokers []string, topic, groupID string) *kafka.Reader {
	return kafka.NewReader(kafka.ReaderConfig{
		Brokers:  brokers,
		GroupID:  groupID,
		Topic:    topic,
		MinBytes: 1,
		MaxBytes: 10e6,
	})
}

func DecodeEnvelope(data []byte) (Envelope, error) {
	var envelope Envelope
	err := json.Unmarshal(data, &envelope)
	return envelope, err
}
