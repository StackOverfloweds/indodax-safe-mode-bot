package helper

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
)

// Global Configuration (Penting untuk penamaan file CSV)
const (
	CSV_FILE_PREFIX = "position_"
)

// Position menyimpan status posisi yang sedang berjalan.
// Struct ini dipindahkan ke sini agar dapat diakses oleh package helper maupun monitor
// tanpa menciptakan circular dependency.
type Position struct {
	Pair     string
	Active   bool
	BuyPrice float64
	Amount   float64
	Time     string
	mu       sync.Mutex
}

// LoadLastPosition memuat posisi terakhir dari file CSV.
func LoadLastPosition(pair string) (*Position, error) {
	filePath := getCSVFilePath(pair)
	file, err := os.Open(filePath)
	if err != nil {
		// File tidak ada/error, kembalikan posisi non-aktif
		return &Position{Pair: pair, Active: false}, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil || len(records) < 2 {
		return &Position{Pair: pair, Active: false}, fmt.Errorf("CSV kosong atau format salah")
	}

	lastRecord := records[len(records)-1]

	active, _ := strconv.ParseBool(lastRecord[1])
	buyPrice, _ := strconv.ParseFloat(lastRecord[2], 64)
	amount, _ := strconv.ParseFloat(lastRecord[3], 64)

	return &Position{
		Pair:     lastRecord[0],
		Active:   active,
		BuyPrice: buyPrice,
		Amount:   amount,
		Time:     lastRecord[4],
	}, nil
}

// SavePosition menyimpan posisi saat ini ke file CSV.
func SavePosition(pos *Position) {
	pos.mu.Lock()
	defer pos.mu.Unlock()

	filePath := getCSVFilePath(pos.Pair)

	// Cek apakah file sudah ada dan punya header
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0644)
	isNewFile := os.IsNotExist(err)

	if isNewFile {
		file, err = os.Create(filePath)
		if err != nil {
			log.Printf("❌ Gagal membuat file CSV: %v", err)
			return
		}
		// Tulis header
		writer := csv.NewWriter(file)
		writer.Write([]string{"Pair", "Active", "BuyPrice", "Amount", "Time"})
		writer.Flush()
	} else if err != nil {
		log.Printf("❌ Gagal membuka file CSV: %v", err)
		return
	}
	defer file.Close()

	record := []string{
		pos.Pair,
		strconv.FormatBool(pos.Active),
		strconv.FormatFloat(pos.BuyPrice, 'f', 8, 64),
		strconv.FormatFloat(pos.Amount, 'f', 8, 64),
		pos.Time,
	}

	writer := csv.NewWriter(file)
	writer.Write(record)
	writer.Flush()
}

func getCSVFilePath(pair string) string {
	return CSV_FILE_PREFIX + pair + ".csv"
}
