package features

import (
	"context"

	"fraud-payments/internal/payments"
	"fraud-payments/internal/store"
)

type Builder struct {
	Store *store.Store
}

func (b *Builder) Build(ctx context.Context, req payments.AuthorizationRequest) (payments.FeatureSnapshot, error) {
	return b.Store.ReadFeatureSnapshot(ctx, req)
}
