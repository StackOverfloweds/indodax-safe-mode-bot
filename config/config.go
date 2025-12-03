package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config berisi semua pengaturan yang dibutuhkan bot
type Config struct {
	// --- API & BASE CONFIG ---
	BaseURL   string
	TapiURL   string
	APIKey    string
	APISecret string
	DryRun    bool

	// --- DL SERVICE CONFIG ---
	DLServiceURL string

	// --- MONITOR & STRATEGY CONFIG ---
	TradingPairs          string  // e.g., "btc_idr, eth_idr"
	PollDelaySeconds      int     // Delay loop utama (e.g., 5)
	BuyCooldownSeconds    int     // Cooldown setelah beli (e.g., 600)
	SellCooldownSeconds   int     // Cooldown setelah jual (e.g., 600)
	DLConfidenceThreshold float64 // Ambang batas probabilitas DL (e.g., 0.8)
	UseSplitOrder         bool    // Menggunakan split order untuk beli
	SplitA                float64 // Persentase split A (e.g., 60)
	SplitB                float64 // Persentase split B (e.g., 40)
	ModalIDR              float64 // Modal IDR tetap (Jika 0, menggunakan saldo IDR)
	MinModal              float64 // Modal IDR minimum untuk trading

	// --- NOTIFICATION CONFIG ---
	TelegramBotToken string
	TelegramChatID   string
}

// getEnvDefaultString mengambil env var atau nilai default
func getEnvDefaultString(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvDefaultInt mengambil env var dan mengkonversinya ke int, atau nilai default
func getEnvDefaultInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

// getEnvDefaultFloat mengambil env var dan mengkonversinya ke float64, atau nilai default
func getEnvDefaultFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	}
	return defaultValue
}

// getEnvDefaultBool mengambil env var dan mengkonversinya ke bool, atau nilai default
func getEnvDefaultBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return defaultValue
}

// Load memuat konfigurasi dari environment variables atau file .env
func Load() *Config {
	// Load .env
	_ = godotenv.Load()

	// 1. API & BASE CONFIG
	dry := getEnvDefaultBool("DRY_RUN", false)

	cfg := &Config{
		BaseURL:   getEnvDefaultString("INDODAX_BASE_URL", "https://indodax.com/api"),
		TapiURL:   getEnvDefaultString("INDODAX_TAPI_URL", "https://indodax.com/tapi"),
		APIKey:    os.Getenv("API_KEY"),
		APISecret: os.Getenv("API_SECRET"),
		DryRun:    dry,

		// 2. DL SERVICE CONFIG
		DLServiceURL: os.Getenv("DL_SERVICE_URL"),

		// 3. MONITOR & STRATEGY CONFIG
		TradingPairs:          getEnvDefaultString("TRADING_PAIRS", "btc_idr"),
		PollDelaySeconds:      getEnvDefaultInt("POLL_DELAY_SECONDS", 5),
		BuyCooldownSeconds:    getEnvDefaultInt("BUY_COOLDOWN_SECONDS", 600),
		SellCooldownSeconds:   getEnvDefaultInt("SELL_COOLDOWN_SECONDS", 600),
		DLConfidenceThreshold: getEnvDefaultFloat("DL_CONFIDENCE_THRESHOLD", 0.8),
		UseSplitOrder:         getEnvDefaultBool("USE_SPLIT_ORDER", false),
		SplitA:                getEnvDefaultFloat("SPLIT_A_PERCENT", 60),
		SplitB:                getEnvDefaultFloat("SPLIT_B_PERCENT", 40),
		ModalIDR:              getEnvDefaultFloat("MODAL_IDR", 0), // 0 berarti menggunakan seluruh saldo IDR
		MinModal:              getEnvDefaultFloat("MIN_MODAL", 20000),

		// 4. NOTIFICATION CONFIG
		TelegramBotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID:   os.Getenv("TELEGRAM_CHAT_ID"),
	}

	// Peningkatan Keamanan: Hentikan program jika API Key/Secret tidak ada dan bukan Dry Run
	if (cfg.APIKey == "" || cfg.APISecret == "") && !cfg.DryRun {
		log.Fatal("Fatal: API Key atau Secret tidak ada. Set DRY_RUN=true atau masukkan kredensial Indodax.")
	}

	// Validasi URL DL
	if cfg.DLServiceURL == "" {
		log.Println("⚠️ Peringatan: DL_SERVICE_URL tidak diset. Fitur DL dinonaktifkan.")
	}

	// Log konfigurasi dasar
	log.Printf("⚙️ Konfigurasi Loaded: DryRun=%t, DL URL=%s, Pairs=%s, PollDelay=%d",
		cfg.DryRun, cfg.DLServiceURL, cfg.TradingPairs, cfg.PollDelaySeconds)

	return cfg
}
