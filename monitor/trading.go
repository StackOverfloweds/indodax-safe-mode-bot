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
	"safe-mode/service/dl" // Import DL Service Interface (dl_service_interface.go)
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

// Config menampung semua parameter strategi trading yang didorong oleh DL
type Config struct {
	Pair                  string
	PollDelay             time.Duration
	BuyCooldown           time.Duration
	SellCooldown          time.Duration
	DLConfidenceThreshold float64 // Ambang batas probabilitas minimum untuk mengeksekusi aksi DL (e.g. 0.8)
	UseSplitOrder         bool
	SplitA                float64 // e.g. 60 (percent)
	SplitB                float64 // e.g. 40 (percent)
	ModalIDR              float64
	MinModal              float64
}

// Monitor adalah mesin utama trading
type Monitor struct {
	Account *account.Service
	Market  *market.Service
	OM      *ordermanager.Service
	DL      *dl.Service // Menggunakan dl.Service
	Cfg     *Config

	priceBuf []float64
	mu       sync.Mutex

	lastBuyTime  time.Time
	lastSellTime time.Time
}

// NewMonitor constructor dengan signature yang diperbarui
func NewMonitor(acc *account.Service, mkt *market.Service, om *ordermanager.Service, dlSvc *dl.Service, cfg *Config) *Monitor {
	if cfg.PollDelay == 0 {
		cfg.PollDelay = 5 * time.Second
	}
	if cfg.SplitA == 0 && cfg.SplitB == 0 {
		cfg.SplitA = 60
		cfg.SplitB = 40
	}
	if cfg.MinModal == 0 {
		cfg.MinModal = 20000
	}
	return &Monitor{
		Account:  acc,
		Market:   mkt,
		OM:       om,
		DL:       dlSvc,
		Cfg:      cfg,
		priceBuf: make([]float64, 0, 500),
	}
}

// --------------------- HELPERS & UTILS ---------------------

func formatIDR(v float64) string { return ordermanager.FormatIDR(v) }

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

func getBalanceFromInfo(info map[string]interface{}, currency string) float64 {
	if info == nil {
		return 0.0
	}
	if ret, ok := info["return"].(map[string]interface{}); ok {
		info = ret
	}
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

// --------------------- INDICATORS & ANALYSIS (DL ONLY) ---------------------

// indicatorStandardDeviation dipertahankan untuk Feature Engineering
func indicatorStandardDeviation(prices []float64) float64 {
	n := len(prices)
	if n < 2 {
		return 0.0
	}
	sum := 0.0
	for _, p := range prices {
		sum += p
	}
	mean := sum / float64(n)
	variance := 0.0
	for _, p := range prices {
		variance += math.Pow(p-mean, 2)
	}
	return math.Sqrt(variance / float64(n))
}

// RSI dipertahankan untuk Feature Engineering
func RSI(prices []float64, period int) float64 {
	n := len(prices)
	if n < period {
		return 50.0
	}

	startIdx := n - period
	if startIdx < 0 {
		startIdx = 0
	}

	gains := 0.0
	losses := 0.0

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
		return 100
	}

	rs := avgGain / avgLoss
	rsi := 100.0 - (100.0 / (1.0 + rs))

	return math.Max(0, math.Min(100, rsi))
}

// --------------------- FEATURE ENGINEERING UNTUK DL ---------------------

func (m *Monitor) formatDataForDL(pnlPct float64) ([][]float64, error) {
	const sequenceLength = 30

	prices := m.getPrices()
	n := len(prices)

	if n < sequenceLength {
		return nil, fmt.Errorf("buffer harga kurang dari %d data", sequenceLength)
	}

	pricesWindow := prices[n-sequenceLength:]
	inputData := make([][]float64, sequenceLength)
	pnlRatio := pnlPct / 100.0

	for i := 0; i < sequenceLength; i++ {
		subWindow := pricesWindow[:i+1]
		currentPrice := pricesWindow[i]
		subN := len(subWindow)

		// 1. Price Change
		priceChange := 0.0
		if i > 0 {
			priceChange = (currentPrice - pricesWindow[i-1]) / pricesWindow[i-1] * 100
		}

		// 2. PnL Ratio (Status posisi saat ini - Statis)
		// Note: PnL Ratio akan 0.0 jika tidak ada posisi aktif.
		// Di Python, ini akan membantu model mengetahui apakah ada posisi yang bisa di-TP/SL.

		// 3. Volatility (Standard Deviation 10 data)
		sdWindow := subWindow
		if subN > 10 {
			sdWindow = subWindow[subN-10:]
		}
		volatility := indicatorStandardDeviation(sdWindow) * 1000

		// 4. RSI (RSI lokal 14 periode)
		localRSI := 50.0
		if subN >= 15 {
			localRSI = RSI(subWindow, 14)
		}

		// 5. Volume (Asumsi 0.0 - jika ada data volume, ganti 0.0 ini)
		volume := 0.0

		inputData[i] = []float64{
			priceChange,
			pnlRatio,
			volatility,
			localRSI,
			volume,
		}
	}

	return inputData, nil
}

// --------------------- LOGGING ---------------------

func logHeader(pair string, tick *market.PublicTicker) {
	log.Println("\n---------------------------------------------")
	log.Printf("Ticker: %s | Harga: Rp %s", strings.ToUpper(pair), formatIDR(tick.Last))
	log.Printf("Buy: %s | Sell: %s", formatIDR(tick.Buy), formatIDR(tick.Sell))
	log.Println("---------------------------------------------")
}

// Mengambil DLResponse sebagai value, bukan pointer.
func logAnalysisDL(coin string, coinBal float64, prices []float64, dlResp dl.DLResponse) {
	log.Printf("   💰 %s Balance: %.8f", strings.ToUpper(coin), coinBal)

	if dlResp.BestAction != "" {
		color := ColorYellow
		if dlResp.BestAction == "BUY" || dlResp.BestAction == "TAKE_PROFIT" {
			color = ColorGreen
		} else if dlResp.BestAction == "SELL" || dlResp.BestAction == "STOP_LOSS" {
			color = ColorRed
		}

		log.Printf("   🧠 DL DECISION: %s%s%s (Prob: %.4f)", color, dlResp.BestAction, ColorReset, dlResp.BestProb)

		// Log probabilitas rinci (hanya untuk debugging)
		probStr := ""
		for action, prob := range dlResp.Probabilities {
			probStr += fmt.Sprintf(" %s: %.2f", action, prob)
		}
		log.Printf("   📊 DL Probs:%s", probStr)
	} else {
		log.Printf("   🧠 DL DECISION: %sERROR%s atau belum ada respons.", ColorRed, ColorReset)
	}
	log.Println("   ---------------------------------------------")
}

func logPosition(pos *helper.Position, pnlPct float64, orderStr string) {
	log.Printf("   ✅ POSISI AKTIF | Beli @ %s (%s) | PnL: %s",
		formatIDR(pos.BuyPrice), pos.Time, colorizePct(pnlPct))
	log.Printf("   📋 Orders: %s", orderStr)
	log.Println("   ---------------------------------------------")
}

// --------------------- DL DECISION ENGINE ---------------------

// Mengambil DLResponse sebagai value, bukan pointer.
func (m *Monitor) handleDLDecision(dlResp dl.DLResponse, pos *helper.Position, openOrders []map[string]interface{}) {

	bestAction := dlResp.BestAction

	// Safety check: Abaikan sinyal jika confidence terlalu rendah
	if dlResp.BestProb < m.Cfg.DLConfidenceThreshold {
		log.Printf("   🧠 DL Filter: Aksi %s DITOLAK karena Probabilitas (%.4f) < Threshold (%.2f).",
			bestAction, dlResp.BestProb, m.Cfg.DLConfidenceThreshold)
		return
	}

	// Pisahkan order berdasarkan tipe
	_, sellOrders, buyOrders := m.OM.FormatOrders(openOrders, m.Cfg.Pair)

	switch bestAction {
	case "BUY":
		// Aksi BUY hanya dilakukan jika TIDAK ADA posisi aktif.
		if pos.Active {
			log.Println("   ⚠️ DL BUY DITOLAK: Posisi sudah aktif. Model mungkin melihat momentum naik.")
			return
		}

		// Cek Cooldown dan Modal
		if !m.allowBuyNow() {
			log.Println("   ⚠️ DL BUY DITOLAK: Masih dalam cooldown.")
			return
		}

		currentModal := m.Cfg.ModalIDR
		if currentModal <= 0 {
			// Jika ModalIDR tidak diset, gunakan saldo IDR saat ini
			ctxInfo, cancelInfo := context.WithTimeout(context.Background(), 10*time.Second)
			info, _ := m.Account.GetInfo(ctxInfo)
			cancelInfo()
			currentModal = getBalanceFromInfo(info, "idr")
		}

		if currentModal < m.Cfg.MinModal {
			log.Printf("   ⚠️ DL BUY DITOLAK: Modal (Rp %s) di bawah minimal (Rp %s).", formatIDR(currentModal), formatIDR(m.Cfg.MinModal))
			return
		}

		// Jika ada order BUY limit aktif, batalkan dulu untuk menghindari over-buy
		if len(buyOrders) > 0 {
			m.OM.CancelActiveOrders(m.Cfg.Pair, buyOrders, "DL_BUY_NEW_SIGNAL")
		}

		orderA, orderB := m.calculateSplitOrders(currentModal)

		// Logika BUY: Gunakan limit order agresif (DL mungkin menyarankan limit atau market, kita pilih limit maker)

		// Asumsi tick.Buy sudah tersedia dari loop utama (Tick.Last)
		prices := m.getPrices()
		lastPrice := prices[len(prices)-1]
		limitPriceA := lastPrice * 0.999 // Sedikit di bawah last price (agresif maker)

		if orderA >= m.Cfg.MinModal {
			log.Printf("   ⬆️ Memasang Limit Buy A (%.0f%%) @ Rp %s (Modal: %s)", m.Cfg.SplitA, formatIDR(limitPriceA), formatIDR(orderA))
			m.OM.PlaceLimitOrder(m.Cfg.Pair, "buy", limitPriceA, orderA)
			m.lastBuyTime = time.Now()
		}

		if orderB >= m.Cfg.MinModal && m.Cfg.UseSplitOrder {
			limitPriceB := lastPrice * 0.997 // Lebih defensif
			log.Printf("   ⬆️ Memasang Limit Buy B (%.0f%%) @ Rp %s (Modal: %s)", m.Cfg.SplitB, formatIDR(limitPriceB), formatIDR(orderB))
			m.OM.PlaceLimitOrder(m.Cfg.Pair, "buy", limitPriceB, orderB)
			m.lastBuyTime = time.Now()
		}

	case "SELL":
		// Aksi SELL (Short Entry) hanya dilakukan jika TIDAK ADA posisi aktif. (Jika ini bot LONG-ONLY, kita abaikan SELL entry)
		log.Println("   ⚠️ DL SELL DITOLAK: Strategi ini adalah Long-Only (tidak ada posisi short entry).")
		// Jika Anda ingin mengimplementasikan short, letakkan logika di sini.

	case "TAKE_PROFIT":
		// Aksi TP hanya dilakukan jika ADA posisi aktif.
		if !pos.Active || !m.allowSellNow() {
			log.Println("   ⚠️ DL TP DITOLAK: Tidak ada posisi aktif atau sedang cooldown.")
			return
		}

		log.Printf("   💰 DL SIGNAL TP (%.4f) -> MarketSell untuk Realisasi Profit.%s", dlResp.BestProb, ColorReset)
		m.OM.MarketSell(m.Cfg.Pair, pos.Amount)
		m.OM.CancelActiveOrders(m.Cfg.Pair, sellOrders, "DL_TAKE_PROFIT")
		m.lastSellTime = time.Now()

	case "STOP_LOSS":
		// Aksi SL hanya dilakukan jika ADA posisi aktif.
		if !pos.Active || !m.allowSellNow() {
			log.Println("   🚨 DL SL DITOLAK: Tidak ada posisi aktif atau sedang cooldown.")
			return
		}

		log.Printf("   🚨 DL SIGNAL SL (%.4f) -> MarketSell untuk Cut Loss (Stop Loss).%s", dlResp.BestProb, ColorReset)
		m.OM.MarketSell(m.Cfg.Pair, pos.Amount)
		m.OM.CancelActiveOrders(m.Cfg.Pair, sellOrders, "DL_STOP_LOSS")
		m.lastSellTime = time.Now()

	case "HOLD":
		log.Println("   🧠 DL menyarankan HOLD. Memastikan tidak ada order limit yang tersisa.")
		// Saat HOLD, kita bisa membatalkan order limit yang tidak dieksekusi untuk efisiensi.
		if len(buyOrders) > 0 {
			m.OM.CancelActiveOrders(m.Cfg.Pair, buyOrders, "DL_HOLD_CANCEL_BUY")
		}
		if len(sellOrders) > 0 {
			m.OM.CancelActiveOrders(m.Cfg.Pair, sellOrders, "DL_HOLD_CANCEL_SELL")
		}

	default:
		log.Printf("   ⚠️ Aksi DL tidak dikenal: %s. Melanjutkan ke siklus berikutnya.", bestAction)
	}
}

// --------------------- MAIN MONITOR LOOP ---------------------

func (m *Monitor) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	pos, err := helper.LoadLastPosition(m.Cfg.Pair)
	if err != nil {
		pos = &helper.Position{Active: false, Pair: m.Cfg.Pair}
	}

	log.Printf("%s=============================================%s", ColorCyan, ColorReset)
	log.Printf("🤖 BOT STARTED: %s%s (DL DRIVEN)", ColorGreen, strings.ToUpper(m.Cfg.Pair)+ColorReset)
	log.Printf("%sCONFIDENCE THRESHOLD:%s %.2f", ColorGreen, ColorReset, m.Cfg.DLConfidenceThreshold)
	log.Printf("%s=============================================%s", ColorCyan, ColorReset)

	coin := strings.Split(m.Cfg.Pair, "_")[0]

	for {
		// Deklarasi variabel di sini untuk menghindari "goto jumps over variable declaration"
		var dlPrediction *dl.DLResponse = nil
		var dlErr error = nil

		select {
		case <-ctx.Done():
			log.Printf("🛑 %s[%s] Monitor loop dihentikan oleh Context.%s", ColorRed, m.Cfg.Pair, ColorReset)
			return
		default:
			// --- 1. Fetch Ticker ---
			ctxTick, cancelTick := context.WithTimeout(context.Background(), 10*time.Second)
			tick, err := m.Market.GetTicker(ctxTick, m.Cfg.Pair)
			cancelTick()
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

			// --- 3. Prep Data & Get DL Prediction ---
			var pnlPct float64
			if pos.Active {
				pnlPct = ((tick.Buy - pos.BuyPrice) / pos.BuyPrice) * 100
			}

			inputData, err := m.formatDataForDL(pnlPct)
			if err != nil {
				logHeader(m.Cfg.Pair, tick)
				log.Printf("   🧠 DL Skipped: %s", err)
				goto nextCycle // Lanjut tanpa prediksi jika data kurang
			}

			// Panggilan ke API Python 5-kelas
			dlPrediction, dlErr = m.DL.PredictAction(inputData)

			// --- 4. Analysis & Logging ---
			logHeader(m.Cfg.Pair, tick)

			// Perbaikan Error 2: Dereference dlPrediction
			if dlPrediction != nil {
				logAnalysisDL(coin, coinBal, m.getPrices(), *dlPrediction)
			} else {
				log.Printf("   🧠 DL DECISION: %sERROR%s atau belum ada respons (Pointer Nil).", ColorRed, ColorReset)
			}

			// --- 5. Position Management & Execution ---

			// 5.1. Passive Position Activation Check (Tetap Penting)
			if !pos.Active && coinBal > 0.00000001 {
				log.Printf("%s[%s] 🎉 ORDER TEREKSEKUSI! Saldo %s terdeteksi (%.8f). Mengaktifkan posisi.%s",
					ColorGreen, m.Cfg.Pair, strings.ToUpper(coin), coinBal, ColorReset)
				pos.Active = true
				pos.Amount = coinBal
				pos.BuyPrice = lastPrice
				pos.Time = time.Now().Format("15:04:05")
				helper.SavePosition(pos)

				m.OM.CancelActiveOrders(m.Cfg.Pair, buyOrders, "POSISI_TERISI_OTOMATIS")
			}

			if pos.Active {
				// HOLDING POSITION LOGIC
				logPosition(pos, pnlPct, orderStr)

				// Cek keluar total
				if coinBal < 0.00000001 {
					log.Printf("%s[%s] 💰 Saldo %s Kosong. Posisi ditutup. Menunggu sinyal beli.%s", ColorGreen, m.Cfg.Pair, strings.ToUpper(coin), ColorReset)
					pos.Active = false
					pos.Amount = 0
					pos.BuyPrice = 0
					helper.SavePosition(pos)
					m.OM.CancelActiveOrders(m.Cfg.Pair, sellOrders, "BAL_KOSONG")
					goto nextCycle
				}

			} else {
				// NOT HOLDING LOGIC: Cek modal
				currentModal := m.Cfg.ModalIDR
				if currentModal <= 0 {
					currentModal = idrBalance
				}

				if currentModal < m.Cfg.MinModal {
					log.Printf("%s   ⚠️ Modal (Rp %s) terlalu kecil. Minimal: Rp %s. Tidak mencoba pasang order baru.%s",
						ColorYellow, formatIDR(currentModal), formatIDR(m.Cfg.MinModal), ColorReset)
					goto nextCycle
				}

				// Log saldo IDR saat tidak ada posisi
				log.Printf("   👀 Saldo IDR: %s", formatIDR(idrBalance))
				log.Println("   ---------------------------------------------")
			}

			// --- 6. EXECUTE DL DECISION ---
			if dlErr == nil && dlPrediction != nil {
				// Perbaikan Error 3: Dereference dlPrediction
				m.handleDLDecision(*dlPrediction, pos, openOrders)
			} else {
				log.Printf("   ❌ DL ERROR: %v. Mengabaikan prediksi kali ini.", dlErr)
			}

		nextCycle:
			time.Sleep(m.Cfg.PollDelay)
		}
	}
}
