package modelclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"fraud-payments/internal/payments"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
}

func (c *Client) Score(ctx context.Context, paymentID string, features payments.FeatureSnapshot) (payments.ModelScoreResponse, error) {
	body, err := json.Marshal(payments.ModelScoreRequest{
		PaymentID: paymentID,
		Features:  features,
	})
	if err != nil {
		return payments.ModelScoreResponse{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/internal/model/score", bytes.NewReader(body))
	if err != nil {
		return payments.ModelScoreResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return payments.ModelScoreResponse{}, err
	}
	defer response.Body.Close()

	if response.StatusCode >= 300 {
		return payments.ModelScoreResponse{}, fmt.Errorf("model service returned %s", response.Status)
	}
	var payload payments.ModelScoreResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return payments.ModelScoreResponse{}, err
	}
	return payload, nil
}
