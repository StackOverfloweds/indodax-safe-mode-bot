package indicator

import (
	"fmt"
	"math"
)

// ANSI colors (exported for callers if needed)
const (
	ColorReset  = "\033[0m"
	ColorGreen  = "\033[32m"
	ColorRed    = "\033[31m"
	ColorYellow = "\033[33m"
	ColorCyan   = "\033[36m"
)

// PriceBuffer holds recent prices
type PriceBuffer struct {
	Size   int
	Values []float64
}

func NewPriceBuffer(size int) *PriceBuffer {
	return &PriceBuffer{
		Size:   size,
		Values: make([]float64, 0, size),
	}
}

func (b *PriceBuffer) Add(price float64) {
	if len(b.Values) >= b.Size {
		b.Values = b.Values[1:]
	}
	b.Values = append(b.Values, price)
}

// EMA calculates exponential moving average on given prices
func EMA(period int, prices []float64) float64 {
	n := len(prices)
	if n == 0 {
		return 0
	}
	if n < period {
		return prices[n-1]
	}
	k := 2.0 / float64(period+1)
	ema := prices[0]
	for i := 1; i < n; i++ {
		ema = (prices[i] * k) + (ema * (1 - k))
	}
	return ema
}

// MACD calculates macd / signal / hist using series approach
func MACD(prices []float64) (float64, float64, float64) {
	n := len(prices)
	if n < 26 {
		return 0, 0, 0
	}

	// build macd series by computing ema12 - ema26 for each point >= 26
	var macdSeries []float64
	for i := 0; i < n; i++ {
		if i+1 < 26 {
			continue
		}
		ema12 := EMA(12, prices[:i+1])
		ema26 := EMA(26, prices[:i+1])
		macdSeries = append(macdSeries, ema12-ema26)
	}

	if len(macdSeries) == 0 {
		return 0, 0, 0
	}

	// signal = EMA(9) of macdSeries
	signal := EMA(9, macdSeries)
	macd := macdSeries[len(macdSeries)-1]
	hist := macd - signal
	return macd, signal, hist
}

// RSI
func RSI(period int, prices []float64) float64 {
	n := len(prices)
	if n <= period {
		return 50
	}
	gains := 0.0
	losses := 0.0
	for i := 1; i < n; i++ {
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
	rsi := 100 - (100 / (1 + rs))
	return math.Max(0, math.Min(100, rsi))
}

// RiskHeatmap
func RiskHeatmap(rsi float64, volatility float64) (int, string) {
	// volatility scaled 0..100 expected
	risk := int((rsi*0.6 + volatility*0.4))
	switch {
	case risk < 30:
		return risk, ColorGreen
	case risk < 60:
		return risk, ColorYellow
	default:
		return risk, ColorRed
	}
}

// PredictTargetPrice - simple linear-ish forecast using strength
func PredictTargetPrice(last, emaFast, emaSlow, macdHist float64) float64 {
	strength := (emaFast - emaSlow) + (macdHist * 2)
	// scale to price: multiplier tuned small to avoid crazy jumps
	return last + (strength * 200)
}

// TrendAnalysis - compose text with color hints
func TrendAnalysis(last, emaFast, emaSlow, rsi, macd, signal, hist float64) string {
	color := ColorYellow
	if emaFast > emaSlow {
		color = ColorGreen
	} else if emaFast < emaSlow {
		color = ColorRed
	}
	text := fmt.Sprintf("%s", color)
	if emaFast > emaSlow {
		text += "⬆ UP (EMA)"
	} else if emaFast < emaSlow {
		text += "⬇ DOWN (EMA)"
	} else {
		text += "➖ SIDEWAYS"
	}
	text += fmt.Sprintf("%s | MACD: %.6f SIG: %.6f HIST: %.6f | RSI: %.1f%s", ColorCyan, macd, signal, hist, rsi, ColorReset)
	return text
}

// StandardDeviation menghitung standard deviation dari array float64
func StandardDeviation(prices []float64) float64 {
	n := len(prices)
	if n <= 1 {
		return 0.0
	}

	// Hitung rata-rata (mean)
	sum := 0.0
	for _, price := range prices {
		sum += price
	}
	mean := sum / float64(n)

	// Hitung varians
	variance := 0.0
	for _, price := range prices {
		variance += math.Pow(price-mean, 2)
	}
	variance /= float64(n)

	// Standard deviation adalah akar kuadrat dari varians
	return math.Sqrt(variance)
}

// CalculateRSI menghitung Relative Strength Index (RSI)
func CalculateRSI(period int, prices []float64) float64 {
	n := len(prices)
	if n < period {
		// Tidak cukup data, kembalikan nilai tengah
		return 50
	}

	// Pastikan kita hanya menggunakan periode yang cukup (misal 14 hari)
	if n > period {
		prices = prices[n-period-1:] // Ambil [period+1] data untuk [period] perubahan
		n = len(prices)
	}

	gains := 0.0
	losses := 0.0
	for i := 1; i < n; i++ {
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
	rsi := 100 - (100 / (1 + rs))
	return math.Max(0, math.Min(100, rsi))
}
