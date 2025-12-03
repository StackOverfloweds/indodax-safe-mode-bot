package main

import (
	"log"
	"os"
	"os/signal"
	"sync"
	"time"

	"safe-mode/config"
	"safe-mode/monitor"
	"safe-mode/ordermanager"
	"safe-mode/service/account"
	"safe-mode/service/indodax"
	"safe-mode/service/market"
)

func main() {
	// rapiin log
	log.SetFlags(log.Ldate | log.Ltime)

	cfg := config.Load()

	// Core service (Indodax)
	indodaxSvc := indodax.NewService(cfg)

	// tg := telegram.NewClient(cfg.TelegramBotToken, cfg.TelegramChatID)

	// Sub services
	marketSvc := market.NewService(indodaxSvc)
	accountSvc := account.NewService(indodaxSvc)

	// Order manager needs account + market + core
	om := ordermanager.NewService(accountSvc, marketSvc, indodaxSvc)

	// Monitor config (sesuaikan modal / pair / delay)
	monCfg := &monitor.Config{
		Pair:      "btc_idr",
		TPPercent: 7.0,
		SLPercent: 4.0,
		PollDelay: 5 * time.Second,

		// === Split Order Mode (Auto Modal) ===
		UseSplitOrder: true, // aktifkan modal split
		SplitA:        60,   // 60%
		SplitB:        40,   // 40%

		// ModalIDR tidak perlu diisi → otomatis mengambil saldo IDR wallet
		ModalIDR: 0,
	}

	// Create monitor instance
	mon := monitor.New(accountSvc, marketSvc, om, monCfg)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		mon.Start()
	}()

	// === Shutdown handler (CTRL+C) ===
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	<-c
	log.Println("⛔ CTRL+C detected. Exiting immediately...")
	os.Exit(0)

}
