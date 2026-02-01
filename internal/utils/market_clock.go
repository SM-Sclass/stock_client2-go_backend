package utils

import "time"

const (
    marketStartMinutes = 9*60 + 15  // 9:15 AM = 555 minutes
    marketEndMinutes   = 15*60 + 30 // 3:30 PM = 930 minutes
)

var ist = time.FixedZone("IST", 5*60*60+30*60) // UTC+5:30 (19800 seconds) ✓

func IsMarketTime() bool {
    now := time.Now().In(ist)
    currentMinutes := now.Hour()*60 + now.Minute()
    return currentMinutes >= marketStartMinutes && currentMinutes <= marketEndMinutes
}

func IsAfterMarketClose() bool {  // Exported + camelCase
    now := time.Now().In(ist)
    currentMinutes := now.Hour()*60 + now.Minute()
    return currentMinutes > marketEndMinutes
}

func IsWeekend() bool {
    now := time.Now().In(ist)
    return now.Weekday() == time.Saturday || now.Weekday() == time.Sunday
}

// Bonus: Full market check
func IsTradingDay() bool {
    return !IsWeekend() && IsMarketTime()
}
