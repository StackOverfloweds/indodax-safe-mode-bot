package ordermanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"safe-mode/service/account"
	"safe-mode/service/indodax"
	"safe-mode/service/market"
)

const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorCyan   = "\033[36m"
)

// Service adalah Order Manager
type Service struct {
	AccountSvc *account.Service
	MarketSvc  *market.Service
	Base       *indodax.Service
}

// NewService constructor
func NewService(accountSvc *account.Service, marketSvc *market.Service, base *indodax.Service) *Service {
	return &Service{
		AccountSvc: accountSvc,
		MarketSvc:  marketSvc,
		Base:       base,
	}
}

// GetOpenOrders mem-forward ke account service
func (s *Service) GetOpenOrders(ctx context.Context, pair string) ([]map[string]interface{}, error) {
	return s.AccountSvc.OpenOrders(ctx, pair)
}

// FormatOrders memformat list order ke string dan memisahkan buy/sell
func (s *Service) FormatOrders(openOrders []map[string]interface{}, pair string) (string, []map[string]interface{}, []map[string]interface{}) {
	if len(openOrders) == 0 {
		return fmt.Sprintf("%s🚫 Tidak ada limit order%s", ColorYellow, ColorReset), nil, nil
	}

	var details []string
	var sellOrders, buyOrders []map[string]interface{}
	coinPrefix := strings.Split(pair, "_")[0]

	for _, o := range openOrders {
		oType := fmt.Sprint(o["type"])

		// price
		var oPrice float64
		switch v := o["price"].(type) {
		case string:
			oPrice, _ = strconv.ParseFloat(v, 64)
		case float64:
			oPrice = v
		}

		// id
		oID := fmt.Sprint(o["order_id"])
		shortID := oID
		if len(oID) > 6 {
			shortID = oID[:6] + "..."
		}

		// amount
		var amountStr string
		keys := []string{
			"order_" + coinPrefix,
			"remain_" + coinPrefix,
			"order_amount",
			"remain_amount",
		}
		for _, k := range keys {
			if v, ok := o[k].(string); ok && v != "" {
				amountStr = v
				break
			}
			if v2, ok := o[k].(float64); ok {
				amountStr = fmt.Sprintf("%.8f", v2)
				break
			}
		}

		typeColor := ColorGreen
		if strings.ToLower(oType) == "sell" {
			typeColor = ColorRed
			sellOrders = append(sellOrders, o)
		} else {
			buyOrders = append(buyOrders, o)
		}

		details = append(details, fmt.Sprintf("%s%s @ %s (Amt:%s) [ID:%s]%s",
			typeColor, strings.ToUpper(oType), FormatIDR(oPrice), amountStr, shortID, ColorReset))
	}

	return strings.Join(details, " | "), sellOrders, buyOrders
}

// PlaceLimitOrder memasang limit order
func (s *Service) PlaceLimitOrder(pair, ttype string, price float64, amount float64) (string, error) {
	priceStr := fmt.Sprintf("%.0f", price)

	var amountStr string
	if strings.ToLower(ttype) == "buy" {
		// for buy, amount is IDR (rounded)
		amountStr = fmt.Sprintf("%.0f", amount)
	} else {
		amountStr = fmt.Sprintf("%.8f", amount)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := s.AccountSvc.Trade(ctx, pair, ttype, priceStr, amountStr)
	if err != nil {
		return "", err
	}

	orderID := fmt.Sprint(resp["order_id"])
	log.Printf("%s[%s] 📝 LIMIT %s @ %s (ID: %s)%s", ColorYellow, pair, strings.ToUpper(ttype), FormatIDR(price), orderID, ColorReset)

	return orderID, nil
}

// CancelActiveOrders cancel list orders
func (s *Service) CancelActiveOrders(pair string, orders []map[string]interface{}, reason string) {
	if len(orders) == 0 {
		return
	}
	log.Printf("%s[%s] 🗑 CANCEL (%s): %d orders%s", ColorBlue, pair, reason, len(orders), ColorReset)
	for _, o := range orders {
		oType := fmt.Sprint(o["type"])
		orderID := fmt.Sprint(o["order_id"])

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err := s.AccountSvc.CancelOrder(ctx, pair, orderID, oType)
		cancel()
		if err != nil {
			log.Printf("%s[%s] ❌ Failed cancel %s: %v%s", ColorRed, pair, orderID, err, ColorReset)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// FormatIDR helper
func FormatIDR(value float64) string {
	integerValue := int64(math.Round(value))
	if integerValue == 0 {
		return "0"
	}
	str := strconv.FormatInt(integerValue, 10)
	var result bytes.Buffer
	n := len(str)
	for i := 0; i < n; i++ {
		if i > 0 && (n-i)%3 == 0 {
			result.WriteString(".")
		}
		result.WriteByte(str[i])
	}
	return result.String()
}

// MarketSell: aggressive limit slightly under bid to behave like market
func (s *Service) MarketSell(pair string, amount float64) (map[string]interface{}, error) {
	if amount <= 0 {
		log.Printf("❌ MarketSell canceled: amount <= 0")
		return nil, fmt.Errorf("invalid amount")
	}

	log.Printf("💥 MARKET SELL %s amount %.8f", pair, amount)

	// DRY-RUN
	if s.Base.DryRun() {
		log.Printf("⚠ DRY-RUN: MarketSell not sent")
		return map[string]interface{}{
			"pair":     pair,
			"type":     "sell",
			"amount":   amount,
			"note":     "dry-run",
			"order_id": "DRYRUN_MS_" + pair,
		}, nil
	}

	// get ticker
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	tick, err := s.MarketSvc.GetTicker(ctx, pair)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("failed get ticker: %w", err)
	}

	price := tick.Buy * 0.999
	priceStr := fmt.Sprintf("%.0f", price)
	coin := strings.Split(pair, "_")[0]
	amountStr := fmt.Sprintf("%.8f", amount)

	params := url.Values{}
	params.Set("method", "trade")
	params.Set("pair", pair)
	params.Set("type", "sell")
	params.Set("price", priceStr)
	params.Set(coin, amountStr)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()

	resp, err := s.Base.PrivatePost(ctx2, params)
	if err != nil {
		log.Printf("❌ MarketSell API error: %v", err)
		return nil, err
	}

	var out map[string]interface{}
	if err := json.Unmarshal(resp.Return, &out); err != nil {
		return nil, err
	}

	log.Printf("✅ MARKET SELL DONE @ %s", FormatIDR(price))
	return out, nil
}
