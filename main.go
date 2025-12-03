package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"safe-mode/config"
	"safe-mode/monitor"
	"safe-mode/ordermanager"
	"safe-mode/service/account"
	"safe-mode/service/dl"
	"safe-mode/service/indodax"
	"safe-mode/service/market"
)

func main() {
	// Merapikan format log sesuai permintaan user
	log.SetFlags(log.Ldate | log.Ltime)
	log.Println("Starting Safe-Mode DL Trading Bot...")

	// 1. Muat Konfigurasi
	cfg := config.Load()

	// 2. Inisialisasi Layanan Dasar (Dependency Injection)
	indodaxSvc := indodax.NewService(cfg)
	marketSvc := market.NewService(indodaxSvc)
	accountSvc := account.NewService(indodaxSvc)
	dlSvc := dl.NewService(cfg)
	om := ordermanager.NewService(accountSvc, marketSvc, indodaxSvc)

	// 3. Konteks untuk Shutdown Graceful (Dideklarasikan SEKALI)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// Menangani sinyal OS (Ctrl+C atau SIGTERM)
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)

	// 4. Proses Setiap Trading Pair dan Jalankan Monitor
	pairs := strings.Split(cfg.TradingPairs, ",")

	if len(pairs) == 0 || (len(pairs) == 1 && strings.TrimSpace(pairs[0]) == "") {
		log.Fatal("❌ Konfigurasi TradingPairs kosong. Mohon atur variabel lingkungan TRADING_PAIRS.")
	}

	for _, p := range pairs {
		pair := strings.TrimSpace(p)
		if pair == "" {
			continue
		}

		log.Printf("🚀 Memulai Monitor untuk Pair: %s", strings.ToUpper(pair))

		// Buat konfigurasi Monitor spesifik untuk pair ini
		monitorCfg := &monitor.Config{
			Pair:                  pair,
			PollDelay:             time.Duration(cfg.PollDelaySeconds) * time.Second,
			BuyCooldown:           time.Duration(cfg.BuyCooldownSeconds) * time.Second,
			SellCooldown:          time.Duration(cfg.SellCooldownSeconds) * time.Second,
			DLConfidenceThreshold: cfg.DLConfidenceThreshold,
			UseSplitOrder:         cfg.UseSplitOrder,
			SplitA:                cfg.SplitA,
			SplitB:                cfg.SplitB,
			ModalIDR:              cfg.ModalIDR,
			MinModal:              cfg.MinModal,
		}

		// Inisialisasi Trading Monitor dengan Service yang sudah di-inject
		mon := monitor.NewMonitor(accountSvc, marketSvc, om, dlSvc, monitorCfg)

		// Mulai Monitor di Goroutine
		wg.Add(1)
		go mon.Start(ctx, &wg)
	}

	// 5. Menunggu Sinyal Shutdown
	<-signalCh
	log.Println("\n👋 Sinyal Interrupt diterima. Memulai Shutdown...")

	// 6. Melakukan Shutdown Graceful
	cancel()  // Beri tahu semua goroutine untuk berhenti
	wg.Wait() // Tunggu hingga semua goroutine selesai

	log.Println("✅ Shutdown Selesai. Bot berhenti.")
}
