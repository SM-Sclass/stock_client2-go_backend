package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/kite"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/repository"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

type InstrumentService struct {
	Kite                  *kite.KiteClient
	Repo                  *repository.InstrumentRepository
	NSEInstruments        []kiteconnect.Instrument
	BSEInstruments        []kiteconnect.Instrument
	NFOInstruments        []kiteconnect.Instrument
	NSESymbolToInstrument map[string]kiteconnect.Instrument
	NSETokenToInstrument  map[uint32]kiteconnect.Instrument
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
	s.BSEInstruments, err = s.Kite.GetInstrumentsByExchange("BSE")
	if err != nil {
		return fmt.Errorf("failed to load BSE instruments: %v", err)
	}
	s.NFOInstruments, err = s.Kite.GetInstrumentsByExchange("NFO")
	if err != nil {
		return fmt.Errorf("failed to load NFO instruments: %v", err)
	}

	return nil
}

func (s *InstrumentService) StoreInstrument() error {
	ctx := context.Background()

	// marshal instruments to JSON
	nseBytes, _ := json.Marshal(s.NSEInstruments)
	bseBytes, _ := json.Marshal(s.BSEInstruments)
	nfoBytes, _ := json.Marshal(s.NFOInstruments)

	if _, err := s.Repo.UpsertInstruments(ctx, "NSE", nseBytes); err != nil {
		return err
	}
	if _, err := s.Repo.UpsertInstruments(ctx, "BSE", bseBytes); err != nil {
		return err
	}
	if _, err := s.Repo.UpsertInstruments(ctx, "NFO", nfoBytes); err != nil {
		return err
	}

	return nil
}

func (s *InstrumentService) LoadInstrumentFromDB() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	exchanges := []string{"NSE", "BSE", "NFO"}

	for _, exchange := range exchanges {
		inst, err := s.Repo.GetInstrumentsByExchange(ctx, exchange)
		if err != nil {
			return fmt.Errorf("failed to get instruments for exchange %s: %v", exchange, err)
		}

		switch exchange {
		case "NSE":
			// deserialize JSON → []kiteconnect.Instrument
			json.Unmarshal(inst.InstrumentsData, &s.NSEInstruments)
		case "BSE":
			json.Unmarshal(inst.InstrumentsData, &s.BSEInstruments)
		case "NFO":
			json.Unmarshal(inst.InstrumentsData, &s.NFOInstruments)
		}
	}

	return nil
}

func (s *InstrumentService) BuildInstrumentMaps() {
	s.NSESymbolToInstrument = make(map[string]kiteconnect.Instrument)
	s.NSETokenToInstrument = make(map[uint32]kiteconnect.Instrument)

	for _, inst := range s.NSEInstruments {
		s.NSESymbolToInstrument[inst.Tradingsymbol] = inst
		s.NSETokenToInstrument[uint32(inst.InstrumentToken)] = inst
	}
}

func (s *InstrumentService) IsInstrumentsDataStale() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	exchanges := []string{"NSE", "BSE", "NFO"}
	now := time.Now().UTC()

	for _, exchange := range exchanges {
		inst, err := s.Repo.GetStoredDateByExchange(ctx, exchange)
		if err != nil {
			return false, fmt.Errorf("failed to get instruments for exchange %s: %v", exchange, err)
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

func (s *InstrumentService) GetSearchedInstrumentByName(name string) (*kiteconnect.Instrument, error) {
	search := strings.ToLower(name)

	for _, inst := range s.NSEInstruments {
		if strings.Contains(strings.ToLower(inst.Name), search) {
			return &inst, nil
		}
	}

	return nil, fmt.Errorf("instrument not found for name: %s", name)
}
