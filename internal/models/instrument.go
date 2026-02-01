package models

import (
	"encoding/json"
	"time"
)

type Instrument struct {
	ID              int64     `json:"id"`
	Exchange        string     `json:"exchange"`
	InstrumentsData json.RawMessage    `json:"instruments_data"`
	StoredAt        time.Time `json:"stored_at"`
}
