package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config berisi semua pengaturan yang dibutuhkan bot
type Config struct {
	BaseURL   string
	TapiURL   string
	APIKey    string
	APISecret string
	DryRun    bool

	TelegramBotToken string
	TelegramChatID   string
}

// Load memuat konfigurasi dari environment variables atau file .env
func Load() *Config {
	// Load .env
	_ = godotenv.Load()

	dry := false
	if v := os.Getenv("DRY_RUN"); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			dry = b
		}
	}

	cfg := &Config{
		BaseURL:   os.Getenv("INDODAX_BASE_URL"),
		TapiURL:   os.Getenv("INDODAX_TAPI_URL"),
		APIKey:    os.Getenv("API_KEY"),
		APISecret: os.Getenv("API_SECRET"),
		DryRun:    dry,
	}

	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://indodax.com/api"
	}
	if cfg.TapiURL == "" {
		cfg.TapiURL = "https://indodax.com/tapi"
	}

	// Peningkatan Keamanan: Hentikan program jika API Key/Secret tidak ada dan bukan Dry Run
	if (cfg.APIKey == "" || cfg.APISecret == "") && !cfg.DryRun {
		log.Fatal("❌ Error: API_KEY or API_SECRET must be set for live trading.")
	} else if cfg.APIKey == "" || cfg.APISecret == "" {
		log.Println("⚠️ Warning: API_KEY or API_SECRET empty — running in Dry Run mode.")
	}

	return cfg
}
