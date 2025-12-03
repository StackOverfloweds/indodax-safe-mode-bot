package monitor

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"safe-mode/helper"
	"safe-mode/ordermanager"
	"safe-mode/service/account"
	"safe-mode/service/market"
)

// --------------------- ANSI COLORS ---------------------
const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorCyan   = "\033[36m"
	ColorPurple = "\033[35m"
	ColorWhite  = "\033[37m"
)

// --------------------- CONFIG & MONITOR ---------------------
type Config struct {
	Pair         string
	TPPercent    float64
	SLPercent    float64
	PollDelay    time.Duration
	BuyCooldown  time.Duration
	SellCooldown time.Duration

	// Split order config
	UseSplitOrder bool
	SplitA        float64 // e.g. 60 (percent)
	SplitB        float64 // e.g. 40 (percent)

	// Modal: if >0 use fixed ModalIDR, if 0 use realtime wallet IDR
	ModalIDR float64

	// Minimal modal to allow placing orders
	MinModal float64
}

type Monitor struct {
	Account *account.Service
	Market  *market.Service
	OM      *ordermanager.Service
	Cfg     *Config

	priceBuf []float64 // Buffer untuk perhitungan indikator
	mu       sync.Mutex

	lastBuyTime  time.Time
	lastSellTime time.Time
}

// New constructor
func New(acc *account.Service, mkt *market.Service, om *ordermanager.Service, cfg *Config) *Monitor {
	// Defaults
	if cfg.PollDelay == 0 {
		cfg.PollDelay = 5 * time.Second
	}
	if cfg.SplitA == 0 && cfg.SplitB == 0 {
		cfg.SplitA = 60
		cfg.SplitB = 40
	}
	if cfg.MinModal == 0 {
		cfg.MinModal = 20000 // default minimal modal 20k
	}
	return &Monitor{
		Account:  acc,
		Market:   mkt,
		OM:       om,
		Cfg:      cfg,
		priceBuf: make([]float64, 0, 500),
	}
}

// --------------------- HELPERS ---------------------

func formatIDR(v float64) string { return ordermanager.FormatIDR(v) }

// colorizePct memberikan warna pada persentase
func colorizePct(pct float64) string {
	switch {
	case pct > 0:
		return fmt.Sprintf("%s+%.2f%%%s", ColorGreen, pct, ColorReset)
	case pct < 0:
		return fmt.Sprintf("%s%.2f%%%s", ColorRed, pct, ColorReset)
	default:
		return fmt.Sprintf("%.2f%%", pct)
	}
}

// getBalanceFromInfo reads balances from account.GetInfo response
func getBalanceFromInfo(info map[string]interface{}, currency string) float64 {
	if info == nil {
		return 0.0
	}
	// Check for nested 'return' -> 'balance' structure
	if ret, ok := info["return"].(map[string]interface{}); ok {
		info = ret
	}
	// Check for 'balance' field
	if bRaw, ok := info["balance"].(map[string]interface{}); ok {
		if v, ok := bRaw[currency]; ok {
			switch t := v.(type) {
			case string:
				f, _ := strconv.ParseFloat(t, 64)
				return f
			case float64:
				return t
			}
		}
	}
	return 0.0
}

func (m *Monitor) addPrice(p float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.priceBuf = append(m.priceBuf, p)
	// Batasi buffer hingga 500 data
	if len(m.priceBuf) > 500 {
		m.priceBuf = m.priceBuf[len(m.priceBuf)-500:]
	}
}

func (m *Monitor) getPrices() []float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]float64, len(m.priceBuf))
	copy(cp, m.priceBuf)
	return cp
}

func (m *Monitor) allowBuyNow() bool {
	if m.Cfg.BuyCooldown <= 0 {
		return true
	}
	return time.Since(m.lastBuyTime) >= m.Cfg.BuyCooldown
}
func (m *Monitor) allowSellNow() bool {
	if m.Cfg.SellCooldown <= 0 {
		return true
	}
	return time.Since(m.lastSellTime) >= m.Cfg.SellCooldown
}

// calculate split orders from totalIDR
func (m *Monitor) calculateSplitOrders(totalIDR float64) (float64, float64) {
	if !m.Cfg.UseSplitOrder {
		return totalIDR, 0
	}
	a := m.Cfg.SplitA
	b := m.Cfg.SplitB
	totalPct := a + b
	if totalPct <= 0 {
		return totalIDR, 0
	}
	orderA := totalIDR * (a / totalPct)
	orderB := totalIDR * (b / totalPct)
	return orderA, orderB
}

// --------------------- INDICATORS ---------------------

// EMA (Exponential Moving Average)
func EMA(prices []float64, period int) float64 {
	n := len(prices)
	if n == 0 {
		return 0
	}
	if n < period {
		return prices[n-1] // Return last price if not enough data
	}
	k := 2.0 / float64(period+1)
	// In a time series analysis, EMA usually starts from the first element of the series.
	// For simplicity with a fixed-size buffer, we use the first element as initial EMA.
	ema := prices[0]
	for i := 1; i < n; i++ {
		ema = (prices[i] * k) + (ema * (1 - k))
	}
	return ema
}

// RSI (Relative Strength Index)
func RSI(prices []float64, period int) float64 {
	n := len(prices)
	if n < period {
		return 50.0 // Default to neutral if not enough data
	}

	// Start calculation from the point where we have a full window
	startIdx := n - period
	if startIdx < 0 {
		startIdx = 0
	}

	gains := 0.0
	losses := 0.0

	// Calculate initial sum of gains and losses over the period
	for i := startIdx + 1; i < n; i++ {
		d := prices[i] - prices[i-1]
		if d > 0 {
			gains += d
		} else {
			losses -= d
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	if avgLoss == 0 {
		return 100 // No losses means infinite RS -> RSI 100
	}

	rs := avgGain / avgLoss
	rsi := 100.0 - (100.0 / (1.0 + rs))

	// Clamp RSI to 0-100 range
	return math.Max(0, math.Min(100, rsi))
}

// MACD (Moving Average Convergence Divergence)
func MACD(prices []float64) (float64, float64, float64) {
	n := len(prices)
	if n < 26 {
		return 0, 0, 0
	}

	// Calculate MACD Line (EMA12 - EMA26)
	ema12 := EMA(prices, 12)
	ema26 := EMA(prices, 26)
	macd := ema12 - ema26

	// Calculate Signal Line (EMA9 of MACD Line)
	var macdSeries []float64
	// Need at least 26 prices for the first MACD point,
	// then 9 more for the first Signal point (total 34)
	if n < 34 {
		return macd, 0, 0
	}

	// Calculate MACD series for the Signal Line EMA
	for i := 25; i < n; i++ {
		// Calculate EMA12 and EMA26 up to index i
		ema12i := EMA(prices[:i+1], 12)
		ema26i := EMA(prices[:i+1], 26)
		macdSeries = append(macdSeries, ema12i-ema26i)
	}

	if len(macdSeries) < 9 {
		return macd, 0, 0
	}

	signal := EMA(macdSeries, 9)

	// Calculate Histogram
	hist := macdSeries[len(macdSeries)-1] - signal

	return macd, signal, hist
}

func PredictTargetPrice(last, emaFast, emaSlow, macdHist float64) float64 {
	// Heuristic strength based on EMA divergence and MACD histogram
	strength := (emaFast - emaSlow) + (macdHist * 2)
	// Scale the prediction (factor 200 is an arbitrary fine-tuning value)
	return last + (strength * 200)
}

// RiskHeatmap provides a simple risk score based on momentum (RSI) and volatility (MACD Hist)
func RiskHeatmap(rsi, volatility float64) (int, string) {
	// Simple weighted score: RSI (60%) + Volatility (40%)
	score := int((rsi*0.6 + volatility*0.4))
	switch {
	case score < 35: // Low Risk / High Opportunity
		return score, ColorGreen
	case score < 65: // Moderate Risk
		return score, ColorYellow
	default: // High Risk / Overbought/Oversold
		return score, ColorRed
	}
}

// TrendText formats the indicators for a single log line
func TrendText(emaFast, emaSlow, rsi, macd, signal, hist float64) string {
	color := ColorYellow
	trend := "Neutral"
	if emaFast > emaSlow {
		color = ColorGreen
		trend = "BULLISH"
	} else if emaFast < emaSlow {
		color = ColorRed
		trend = "BEARISH"
	}
	return fmt.Sprintf("%sTrend: %s | EMA: %.2f/%.2f | MACD Hist: %.6f | RSI: %.1f%s",
		color, trend, emaFast, emaSlow, hist, rsi, ColorReset)
}

// --------------------- DECISION ENGINE ---------------------

type Signals struct {
	StrongBuy  bool
	WeakBuy    bool
	StrongSell bool
	WeakSell   bool

	Confidence  float64
	TargetPrice float64

	// Raw indicator values
	EMAFast  float64
	EMASlow  float64
	MACD     float64
	MACDSig  float64
	MACDHist float64
	RSI      float64
}

// evaluateSignals computes buy/sell signals based on indicator combination
func evaluateSignals(prices []float64, last float64) Signals {
	var s Signals
	if len(prices) < 34 { // Min data required for MACD Signal Line
		return s
	}

	// Use slightly shorter periods for faster reaction
	emaFast := EMA(prices, 5)
	emaSlow := EMA(prices, 10)
	rsi := RSI(prices, 6)
	macd, sig, hist := MACD(prices)
	target := PredictTargetPrice(last, emaFast, emaSlow, hist)

	s.EMAFast = emaFast
	s.EMASlow = emaSlow
	s.MACD = macd
	s.MACDSig = sig
	s.MACDHist = hist
	s.RSI = rsi
	s.TargetPrice = target

	// Confidence Heuristic: higher confidence when EMA divergence is large and MACD Histogram is strong
	emaDiff := math.Abs(emaFast-emaSlow) / math.Max(1, last)
	conf := math.Tanh(emaDiff*1000) + math.Min(1.0, math.Abs(hist)*50)
	s.Confidence = math.Min(1, conf) // Cap confidence at 1.0

	// Signal Definitions
	s.StrongBuy = emaFast > emaSlow && hist > 0 && rsi < 70 && s.Confidence > 0.4
	s.WeakBuy = emaFast > emaSlow && hist > -0.0001 && rsi < 75
	s.StrongSell = emaFast < emaSlow && hist < 0 && rsi > 30 && s.Confidence > 0.4
	s.WeakSell = emaFast < emaSlow && hist < 0.0001

	return s
}

// --------------------- LOGGING ---------------------

// logHeader logs the current price status
func logHeader(pair string, tick *market.PublicTicker) {
	lastPrice := tick.Last
	high24 := tick.High
	low24 := tick.Low

	// Determine price movement color based on position in 24h range
	priceColor := ColorWhite
	if lastPrice > (high24+low24)/2 {
		priceColor = ColorGreen
	} else if lastPrice < (high24+low24)/2 {
		priceColor = ColorRed
	}

	headerLog := fmt.Sprintf("%s[%s]%s %sLast: Rp %s%s  (H: %s | L: %s)",
		ColorCyan, strings.ToUpper(pair), ColorReset,
		priceColor, formatIDR(lastPrice), ColorReset,
		formatIDR(high24), formatIDR(low24),
	)
	log.Println(headerLog)
}

// logAnalysis logs the indicator and risk analysis
func logAnalysis(coin string, coinBal float64, prices []float64, lastPrice float64, orderStr string) {
	if len(prices) < 34 {
		need := 34 - len(prices) // Need 34 for full MACD analysis
		log.Printf("   💰 %s Balance: %.8f", strings.ToUpper(coin), coinBal)
		log.Printf("   📊 %sMASIH MENGUMPULKAN DATA%s (%d data harga lagi diperlukan)",
			ColorYellow, ColorReset, need)
		log.Printf("   🎯 Prediksi target: %s-%s", ColorBlue, ColorReset)
		log.Printf("   🔥 Risiko: %s-%s", ColorBlue, ColorReset)
	} else {
		emaFast := EMA(prices, 5)
		emaSlow := EMA(prices, 10)
		rsi := RSI(prices, 6)
		macd, signal, hist := MACD(prices)
		target := PredictTargetPrice(lastPrice, emaFast, emaSlow, hist)
		vol := math.Abs(hist) * 100
		riskScore, riskColor := RiskHeatmap(rsi, vol)

		log.Printf("   💰 %s Balance: %.8f", strings.ToUpper(coin), coinBal)
		log.Printf("   📊 %s", TrendText(emaFast, emaSlow, rsi, macd, signal, hist))
		log.Printf("   🎯 Prediksi target: Rp %s", formatIDR(target))
		log.Printf("   🔥 Risiko: %s%d/100%s (Confidence: %.2f)", riskColor, riskScore, ColorReset, evaluateSignals(prices, lastPrice).Confidence)
	}
	log.Printf("   📋 Orders: %s", orderStr)
	log.Println("   ---------------------------------------------")
}

// logPosition logs the current holding status
func logPosition(pos *helper.Position, pnlPct float64, targetTP, targetSL float64, orderStr string) {
	log.Printf("   %s👉 HOLDING:%s Entry: %s | PnL: %s",
		ColorPurple, ColorReset, formatIDR(pos.BuyPrice), colorizePct(pnlPct))
	log.Printf("   🎯 TP Target: %s | 🛡️ SL Target: %s", formatIDR(targetTP), formatIDR(targetSL))
	log.Printf("   📋 Orders: %s", orderStr)
	log.Println("   ---------------------------------------------")
}

// --------------------- MAIN MONITOR LOOP ---------------------

func (m *Monitor) Start() {
	// Load last saved position
	pos, err := helper.LoadLastPosition(m.Cfg.Pair)
	if err != nil {
		pos = &helper.Position{Active: false, Pair: m.Cfg.Pair}
	}

	log.Printf("%s=============================================%s", ColorCyan, ColorReset)
	log.Printf("🤖 BOT STARTED: %s%s", ColorGreen, strings.ToUpper(m.Cfg.Pair)+ColorReset)
	log.Printf("%sTP:%s %.1f%% | %sSL:%s %.1f%% | Split: %.0f/%.0f",
		ColorGreen, ColorReset, m.Cfg.TPPercent, ColorRed, ColorReset, m.Cfg.SLPercent, m.Cfg.SplitA, m.Cfg.SplitB)
	log.Printf("%s=============================================%s", ColorCyan, ColorReset)

	coin := strings.Split(m.Cfg.Pair, "_")[0]

	for {
		// --- 1. Fetch Ticker ---
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		tick, err := m.Market.GetTicker(ctx, m.Cfg.Pair)
		cancel()
		if err != nil {
			log.Printf("%s[%s] ❌ Error Ticker: %v%s", ColorRed, m.Cfg.Pair, err, ColorReset)
			time.Sleep(m.Cfg.PollDelay)
			continue
		}
		lastPrice := tick.Last
		m.addPrice(lastPrice)

		// --- 2. Fetch Orders & Balances ---
		ctxOrder, cancelOrder := context.WithTimeout(context.Background(), 10*time.Second)
		openOrders, _ := m.OM.GetOpenOrders(ctxOrder, m.Cfg.Pair)
		cancelOrder()

		ctxInfo, cancelInfo := context.WithTimeout(context.Background(), 10*time.Second)
		info, _ := m.Account.GetInfo(ctxInfo)
		cancelInfo()

		idrBalance := getBalanceFromInfo(info, "idr")
		coinBal := getBalanceFromInfo(info, coin)
		orderStr, sellOrders, buyOrders := m.OM.FormatOrders(openOrders, m.Cfg.Pair)
		prices := m.getPrices()

		// --- 3. Analysis & Logging ---
		logHeader(m.Cfg.Pair, tick)
		logAnalysis(coin, coinBal, prices, lastPrice, orderStr)
		signals := evaluateSignals(prices, lastPrice)

		// --- 4. Position Management ---

		// 4.1. Passive Position Activation Check
		// Cek jika saldo koin terdeteksi padahal status bot belum active (terisi manual/limit buy terisi)
		if !pos.Active && coinBal > 0.00000001 { // Check for non-zero balance
			log.Printf("%s[%s] 🎉 ORDER TEREKSEKUSI! Saldo %s terdeteksi (%.8f). Mengaktifkan posisi.%s",
				ColorGreen, m.Cfg.Pair, strings.ToUpper(coin), coinBal, ColorReset)
			pos.Active = true
			pos.Amount = coinBal
			pos.BuyPrice = lastPrice // Use current last price as proxy entry
			pos.Time = time.Now().Format("15:04:05")
			helper.SavePosition(pos)

			// Batalkan semua Limit Buy yang mungkin tersisa
			m.OM.CancelActiveOrders(m.Cfg.Pair, buyOrders, "POSISI_TERISI_OTOMATIS")
		}

		if pos.Active {
			// HOLDING POSITION LOGIC
			pnlPct := ((tick.Buy - pos.BuyPrice) / pos.BuyPrice) * 100
			targetPriceTP := pos.BuyPrice * (1 + (m.Cfg.TPPercent / 100))
			targetPriceSL := pos.BuyPrice * (1 - (m.Cfg.SLPercent / 100))

			logPosition(pos, pnlPct, targetPriceTP, targetPriceSL, orderStr)

			// Check for full exit (saldo coin 0)
			if coinBal < 0.00000001 {
				log.Printf("%s[%s] 💰 Saldo %s Kosong. Posisi ditutup. Menunggu sinyal beli.%s", ColorGreen, m.Cfg.Pair, strings.ToUpper(coin), ColorReset)
				pos.Active = false
				pos.Amount = 0
				pos.BuyPrice = 0
				helper.SavePosition(pos)
				m.OM.CancelActiveOrders(m.Cfg.Pair, sellOrders, "BAL_KOSONG")
				goto nextCycle
			}

			// --- TP/SL Limit Order Placement ---

			// Batalkan pending sell orders jika harga kembali ke zona netral (untuk re-entry/re-positioning)
			isNeutral := pnlPct > -(m.Cfg.SLPercent*0.2) && pnlPct < (m.Cfg.TPPercent*0.2)
			if len(sellOrders) > 0 && isNeutral && signals.Confidence < 0.3 {
				m.OM.CancelActiveOrders(m.Cfg.Pair, sellOrders, "HARGA_NETRAL_CONF_RENDAH")
			}

			// Place TP Limit Order
			if pnlPct >= (m.Cfg.TPPercent*0.2) && len(sellOrders) == 0 && m.allowSellNow() {
				log.Printf("   ⬆️ Memasang Limit TP pada Rp %s", formatIDR(targetPriceTP))
				m.OM.PlaceLimitOrder(m.Cfg.Pair, "sell", targetPriceTP, pos.Amount)
				m.lastSellTime = time.Now()
			}

			// Place SL Limit Order (hanya jika PnL sudah minus signifikan)
			if pnlPct <= -(m.Cfg.SLPercent*0.2) && len(sellOrders) == 0 && m.allowSellNow() {
				log.Printf("   ⬇️ Memasang Limit SL pada Rp %s", formatIDR(targetPriceSL))
				m.OM.PlaceLimitOrder(m.Cfg.Pair, "sell", targetPriceSL, pos.Amount)
				m.lastSellTime = time.Now()
			}

			// --- Smart Exit / Cancel ---

			// Smart cancel TP/SL if indicators flip strongly
			if (signals.StrongSell && pnlPct > 0) || (signals.StrongBuy && pnlPct < 0) {
				if len(sellOrders) > 0 {
					log.Printf("   ⚠️ Indicators flipped strongly -> cancelling all sell limits for manual action.")
					m.OM.CancelActiveOrders(m.Cfg.Pair, sellOrders, "INDICATORS_FLIP")
				}
			}

			// Immediate market sell if strong bearish despite small PnL (cut loss early)
			if signals.StrongSell && pnlPct < 0.5 && m.allowSellNow() {
				log.Printf("   ⚠️ Strong bearish signal detected -> MarketSell immediate (CUT LOSS)")
				m.OM.MarketSell(m.Cfg.Pair, pos.Amount)
				m.OM.CancelActiveOrders(m.Cfg.Pair, sellOrders, "MARKETSELL_STRONG_BEAR")
				// Position will be closed in the next cycle's 'coinBal < 0.00000001' check
				m.lastSellTime = time.Now()
			}

		} else {
			// NOT HOLDING LOGIC: BUY ENTRY

			// --- 5. Modal Calculation (for check/logging) ---
			// currentModal adalah jumlah IDR yang akan digunakan untuk entry kali ini.
			// Jika Cfg.ModalIDR > 0: menggunakan modal tetap (Fixed Modal).
			// Jika Cfg.ModalIDR <= 0: menggunakan seluruh saldo IDR yang tersedia di wallet (Dynamic Modal).
			currentModal := m.Cfg.ModalIDR
			if currentModal <= 0 {
				currentModal = idrBalance
			}

			// Simple Safety Filter: Entry only if price is below 24h average and not near 24h high
			avg24 := (tick.High + tick.Low) / 2
			isSafeEntry := lastPrice < avg24 && lastPrice < (tick.High*0.98)

			signalStr := fmt.Sprintf("%sWAITING%s", ColorYellow, ColorReset)
			if signals.StrongBuy {
				signalStr = fmt.Sprintf("%sSTRONG BUY%s", ColorGreen, ColorReset)
			} else if signals.WeakBuy {
				signalStr = fmt.Sprintf("%sWEAK BUY%s", ColorGreen, ColorReset)
			}

			log.Printf("   👀 Signal: %s | Saldo IDR: %s | Coin: %.8f", signalStr, formatIDR(idrBalance), coinBal)
			log.Printf("   ---------------------------------------------")

			// --- A. PENDING BUY ORDER MANAGEMENT (Penting: Selalu dijalankan jika ada order) ---
			// Jika ada Limit Buy Order, fokus pada monitoring dan pembatalan (jika perlu), JANGAN pasang order baru.
			if len(buyOrders) > 0 {
				log.Printf("   ⏳ %d Limit Buy Orders aktif. Memantau posisi order, tidak mencoba entry baru.", len(buyOrders))

				// Get reference price of the first buy order
				var orderPrice float64
				if ps, ok := buyOrders[0]["price"].(string); ok {
					if v, err := strconv.ParseFloat(ps, 64); err == nil {
						orderPrice = v
					}
				} else if pf, ok := buyOrders[0]["price"].(float64); ok {
					orderPrice = pf
				}

				if orderPrice > 0 {
					// Cancel if market moved up significantly (0.5%)
					if lastPrice > orderPrice*1.005 {
						log.Printf("   ⚠️ Harga menjauh dari limit buy -> membatalkan.")
						m.OM.CancelActiveOrders(m.Cfg.Pair, buyOrders, "HARGA_MENJAUH")
					}
					// Cancel if signals turn strongly bearish
					if signals.StrongSell {
						log.Printf("   ⚠️ Sinyal StrongSell -> membatalkan limit buy.")
						m.OM.CancelActiveOrders(m.Cfg.Pair, buyOrders, "SIGNAL_WEAKEN")
					}
				}
				// Setelah memproses order, kita kembali ke siklus berikutnya tanpa mencoba entry baru.
				goto nextCycle
			}

			// --- B. NEW ENTRY CHECK AND PLACEMENT (Hanya berjalan jika TIDAK ada Limit Buy aktif) ---

			// Safety check: Minimal modal required for new entry
			if currentModal < m.Cfg.MinModal {
				log.Printf("%s   ⚠️ Modal yang akan digunakan (Rp %s) terlalu kecil. Minimal: Rp %s. Tidak mencoba pasang order baru.%s",
					ColorYellow, formatIDR(currentModal), formatIDR(m.Cfg.MinModal), ColorReset)
				goto nextCycle
			}

			// calculate split
			orderA, orderB := m.calculateSplitOrders(currentModal)

			// Place Limit Buy Orders
			if (signals.StrongBuy || (signals.WeakBuy && isSafeEntry)) && idrBalance >= m.Cfg.MinModal && m.allowBuyNow() {

				// Limit Price 1 (aggressive maker)
				limitPriceA := tick.Buy * 0.999
				if orderA >= m.Cfg.MinModal {
					log.Printf("   ⬆️ Memasang Limit Buy A (%.0f%%) @ Rp %s (Modal: %s)", m.Cfg.SplitA, formatIDR(limitPriceA), formatIDR(orderA))
					m.OM.PlaceLimitOrder(m.Cfg.Pair, "buy", limitPriceA, orderA)
					m.lastBuyTime = time.Now()
				}

				// Limit Price 2 (lower price, more defensive)
				if orderB >= m.Cfg.MinModal && m.Cfg.UseSplitOrder {
					limitPriceB := tick.Buy * 0.997
					log.Printf("   ⬆️ Memasang Limit Buy B (%.0f%%) @ Rp %s (Modal: %s)", m.Cfg.SplitB, formatIDR(limitPriceB), formatIDR(orderB))
					m.OM.PlaceLimitOrder(m.Cfg.Pair, "buy", limitPriceB, orderB)
					m.lastBuyTime = time.Now()
				}
			}
		}

	nextCycle:
		time.Sleep(m.Cfg.PollDelay)
	}
}
