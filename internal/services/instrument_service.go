package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/kite"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/repository"
	"github.com/jackc/pgx/v5"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/zerodha/gokiteconnect/v4/models"
)

type InstrumentService struct {
	Kite                  *kite.KiteClient
	Repo                  *repository.InstrumentRepository
	NSEInstruments        kiteconnect.Instruments
	NSESymbolToInstrument map[string]kiteconnect.Instrument
	NSETokenToInstrument  map[uint32]kiteconnect.Instrument
}

type DBInstrument struct {
	InstrumentToken int     `json:"InstrumentToken"`
	ExchangeToken   int     `json:"ExchangeToken"`
	Tradingsymbol   string  `json:"Tradingsymbol"`
	Name            string  `json:"Name"`
	LastPrice       float64 `json:"LastPrice"`
	Expiry          string  `json:"Expiry"` // string instead of time.Time
	StrikePrice     float64 `json:"StrikePrice"`
	TickSize        float64 `json:"TickSize"`
	LotSize         float64 `json:"LotSize"`
	InstrumentType  string  `json:"InstrumentType"`
	Segment         string  `json:"Segment"`
	Exchange        string  `json:"Exchange"`
}

func (s *InstrumentService) InitializeService() {
	stale, err := s.IsInstrumentsDataStale()
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		log.Printf("⚠️ failed to check instrument staleness: %v", err)
	}
	if stale || (err != nil && errors.Is(err, pgx.ErrNoRows)) {
		err := s.FetchAndLoadInstruments()
		if err != nil {
			log.Printf("❌ failed to fetch/load instruments from Kite: %v", err)
			return
		}
		err = s.StoreInstrument()
		if err != nil {
			log.Printf("❌ failed to persist instruments: %v", err)
			return
		}
	} else {
		log.Println("📦 loading instruments from DB cache")
		err = s.LoadInstrumentFromDB()
		if err != nil {
			log.Printf("❌ failed to load instruments from DB: %v", err)
		}
	}
	s.BuildInstrumentMaps()
}

func (s *InstrumentService) FetchAndLoadInstruments() error {
	if valid := s.Kite.IsTokenValid(); !valid {
		return fmt.Errorf("kite access token is invalid")
	}

	var err error
	s.NSEInstruments, err = s.Kite.GetInstrumentsByExchange("NSE")
	if err != nil {
		return fmt.Errorf("failed to load NSE instruments: %v", err)
	}

	log.Printf("✅ fetched %d NSE instruments from Kite", len(s.NSEInstruments))

	return nil
}

func (s *InstrumentService) StoreInstrument() error {
	ctx := context.Background()

	// marshal instruments to JSON
	nseBytes, _ := json.Marshal(s.NSEInstruments)
	if _, err := s.Repo.UpsertInstruments(ctx, "NSE", nseBytes); err != nil {
		return err
	}

	log.Printf("💾 stored %d NSE instruments (%d bytes) in DB", len(s.NSEInstruments), len(nseBytes))

	return nil
}

func (s *InstrumentService) LoadInstrumentFromDB() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// exchanges := []string{"NSE"}
	inst, err := s.Repo.GetInstrumentsByExchange(ctx, "NSE")
	if err != nil {
		return fmt.Errorf("failed to get instruments for exchange %s: %v", "NSE", err)
	}

	// deserialize JSON → []kiteconnect.Instrument
	// json.Unmarshal(inst.InstrumentsData, &s.NSEInstruments)
	// s.SaveInstrumentsToFile()
	var temp []DBInstrument

	err = json.Unmarshal(inst.InstrumentsData, &temp)
	// data, err := json.MarshalIndent(temp, "", "  ")
	if err != nil {
		return err
	}

	// Write JSON to file
	// err = os.WriteFile("instrument.json", data, 0644)
	// if err != nil {
	// 	return fmt.Errorf("unmarshal failed: %v", err)
	// }

	s.NSEInstruments = make(kiteconnect.Instruments, len(temp))

	for i, dbInst := range temp {

		var expiry models.Time
		if dbInst.Expiry != "" {
			t, err := time.Parse(time.RFC3339, dbInst.Expiry)
			if err == nil {
				expiry = models.Time{Time: t}
			}
		}

		s.NSEInstruments[i] = kiteconnect.Instrument{
			InstrumentToken: dbInst.InstrumentToken,
			ExchangeToken:   dbInst.ExchangeToken,
			Tradingsymbol:   dbInst.Tradingsymbol,
			Name:            dbInst.Name,
			Expiry:          expiry,
			StrikePrice:     dbInst.StrikePrice,
			TickSize:        dbInst.TickSize,
			LotSize:         dbInst.LotSize,
			InstrumentType:  dbInst.InstrumentType,
			Segment:         dbInst.Segment,
			Exchange:        dbInst.Exchange,
		}
	}

	log.Printf("✅ loaded %d NSE instruments from DB", len(s.NSEInstruments))

	return nil
}

func (s *InstrumentService) BuildInstrumentMaps() {
	s.NSESymbolToInstrument = make(map[string]kiteconnect.Instrument)
	s.NSETokenToInstrument = make(map[uint32]kiteconnect.Instrument)

	log.Printf("🧭 building symbol/token index maps for %d NSE instruments", len(s.NSEInstruments))

	for _, inst := range s.NSEInstruments {
		s.NSESymbolToInstrument[inst.Tradingsymbol] = inst
		s.NSETokenToInstrument[uint32(inst.InstrumentToken)] = inst
	}
}

func (s *InstrumentService) IsInstrumentsDataStale() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	exchanges := []string{"NSE"}
	now := time.Now().UTC()

	for _, exchange := range exchanges {
		inst, err := s.Repo.GetStoredDateByExchange(ctx, exchange)
		if err != nil {
			return false, err
		}
		stored := inst.StoredAt.UTC()

		sameDay :=
			now.Year() == stored.Year() &&
				now.Month() == stored.Month() &&
				now.Day() == stored.Day()

		if !sameDay {
			return true, nil
		}
	}

	return false, nil
}

func (s *InstrumentService) GetSearchedInstrumentByName(name string) ([]kiteconnect.Instrument, error) {
	search := strings.ToUpper(name)
	var results []kiteconnect.Instrument

	log.Printf("🔎 searching in %d NSE instruments", len(s.NSEInstruments))

	for _, inst := range s.NSEInstruments {
		if strings.Contains(inst.Name, search) {
			results = append(results, inst)
		}
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("instrument not found for name: %s", name)
	}
	return results, nil
}

// func (s *InstrumentService) SaveInstrumentsToFile() error {

// 	// Convert struct slice → JSON bytes
// 	data, err := json.MarshalIndent(s.NSEInstruments, "", "  ")
// 	if err != nil {
// 		return err
// 	}

// 	// Write JSON to file
// 	err = os.WriteFile("instrument.json", data, 0644)
// 	if err != nil {
// 		return err
// 	}

// 	return nil
// }
