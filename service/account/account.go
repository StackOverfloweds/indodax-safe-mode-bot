package account

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"safe-mode/service/indodax"
)

// Service adalah sub-service untuk Account & Trading
type Service struct {
	base *indodax.Service
}

// NewService membuat instance Account Service
func NewService(base *indodax.Service) *Service {
	return &Service{base: base}
}

// GetInfo mengambil informasi akun, termasuk saldo
func (s *Service) GetInfo(ctx context.Context) (map[string]interface{}, error) {
	params := url.Values{}
	params.Set("method", "getInfo")

	apiResp, err := s.base.PrivatePost(ctx, params)
	if err != nil {
		return nil, err
	}

	var ret map[string]interface{}
	if err := json.Unmarshal(apiResp.Return, &ret); err != nil {
		return nil, err
	}
	return ret, nil
}

// Trade melakukan order beli (buy) atau jual (sell) - Sekarang digunakan untuk Limit Order
// Mengembalikan data respon termasuk 'order_id'
func (s *Service) Trade(ctx context.Context, pair, ttype, price, amountOrIDR string) (map[string]interface{}, error) {
	if s.base.DryRun() {
		return map[string]interface{}{
			"pair":       pair,
			"type":       ttype,
			"price":      price,
			"amount/idr": amountOrIDR,
			"note":       "dry-run only",
			"order_id":   "DRY_RUN_" + pair + "_" + ttype, // Placeholder ID
		}, nil
	}

	params := url.Values{}
	params.Set("method", "trade")
	params.Set("pair", pair)
	params.Set("type", ttype)
	params.Set("price", price)

	parts := strings.Split(pair, "_")
	coin := parts[0]

	if ttype == "buy" {
		params.Set("idr", amountOrIDR)
	} else {
		params.Set(coin, amountOrIDR)
	}

	apiResp, err := s.base.PrivatePost(ctx, params)
	if err != nil {
		return nil, err
	}

	var ret map[string]interface{}
	if err := json.Unmarshal(apiResp.Return, &ret); err != nil {
		return nil, err
	}
	return ret, nil
}

// TradeHistory mengambil riwayat trading
func (s *Service) TradeHistory(ctx context.Context, pair string) ([]map[string]interface{}, error) {
	params := url.Values{}
	params.Set("method", "tradeHistory")
	params.Set("pair", pair)

	apiResp, err := s.base.PrivatePost(ctx, params)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Trades []map[string]interface{} `json:"trades"`
	}

	if err := json.Unmarshal(apiResp.Return, &parsed); err != nil {
		return nil, err
	}

	return parsed.Trades, nil
}

// OpenOrders mengambil daftar limit order yang sedang aktif (Pending)
func (s *Service) OpenOrders(ctx context.Context, pair string) ([]map[string]interface{}, error) {
	params := url.Values{}
	params.Set("method", "openOrders")
	params.Set("pair", pair)

	apiResp, err := s.base.PrivatePost(ctx, params)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Orders []map[string]interface{} `json:"orders"`
	}

	if err := json.Unmarshal(apiResp.Return, &parsed); err != nil {
		return nil, nil // Return empty list on parsing error (e.g., empty return body)
	}

	return parsed.Orders, nil
}

// CancelOrder membatalkan order berdasarkan ID dan Pair
func (s *Service) CancelOrder(ctx context.Context, pair, orderID, ttype string) (map[string]interface{}, error) {
	if s.base.DryRun() {
		return map[string]interface{}{
			"order_id": orderID,
			"success":  1,
			"note":     "dry-run canceled",
		}, nil
	}

	params := url.Values{}
	params.Set("method", "cancelOrder")
	params.Set("pair", pair)
	params.Set("order_id", orderID)
	params.Set("type", ttype) // type: 'buy' atau 'sell'

	apiResp, err := s.base.PrivatePost(ctx, params)
	if err != nil {
		return nil, err
	}

	var ret map[string]interface{}
	if err := json.Unmarshal(apiResp.Return, &ret); err != nil {
		return nil, err
	}
	return ret, nil
}
