package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"safe-mode/service/indodax" // Impor package inti indodax
)

// PublicTicker adalah data ticker/harga pasar Indodax
type PublicTicker struct {
	Last float64 `json:"last,string"`
	High float64 `json:"high,string"`
	Low  float64 `json:"low,string"`
	Buy  float64 `json:"buy,string"`
	Sell float64 `json:"sell,string"`
}

// Service adalah sub-service untuk Market Data
type Service struct {
	base *indodax.Service // Embed Service inti
}

// NewService membuat instance Market Service
func NewService(base *indodax.Service) *Service {
	return &Service{base: base}
}

// GetTicker mengambil data ticker publik
func (s *Service) GetTicker(ctx context.Context, pair string) (*PublicTicker, error) {
	// Gunakan BaseURL dan Client dari service inti
	url := fmt.Sprintf("%s/%s/ticker", s.base.BaseURL(), pair)

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := s.base.Client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var wrapper struct {
		Ticker PublicTicker `json:"ticker"`
	}

	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("parse ticker error: %w", err)
	}

	return &wrapper.Ticker, nil
}
