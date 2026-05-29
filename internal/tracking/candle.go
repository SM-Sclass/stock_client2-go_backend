package tracking

// Candle holds the OHLC values for a single time interval.
type Candle struct {
	Open  float64
	High  float64
	Low   float64
	Close float64
}

// Update incorporates a new tick price into the candle.
// The first call sets Open/High/Low; every call updates Close.
func (c *Candle) Update(price float64) {
	if c.Open == 0 {
		c.Open = price
		c.High = price
		c.Low = price
	} else {
		if price > c.High {
			c.High = price
		}
		if price < c.Low {
			c.Low = price
		}
	}
	c.Close = price
}

// IsValid reports whether the candle has received at least one tick.
func (c *Candle) IsValid() bool {
	return c.Open != 0
}

// CandleState holds the rolling Current and Previous 5-min candles for a stock.
type CandleState struct {
	Current  Candle
	Previous Candle
}

// Roll closes the current candle: moves it into Previous and resets Current for the next interval.
func (cs *CandleState) Roll() {
	cs.Previous = cs.Current
	cs.Current = Candle{}
}
