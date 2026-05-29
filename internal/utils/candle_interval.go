package utils

import "time"

type Interval struct {
	Start time.Time
	End   time.Time
}

func GetCompletedIntervals(now time.Time) []Interval {

	loc := now.Location()

	startOfDay := time.Date(
		now.Year(),
		now.Month(),
		now.Day(),
		9, 30, 0, 0,
		loc,
	)

	var intervals []Interval

	current := startOfDay

	for {
		next := current.Add(5 * time.Minute)

		if next.After(now) {
			break
		}

		intervals = append(intervals, Interval{
			Start: current,
			End:   next,
		})

		current = next
	}

	return intervals
}

func GetCurrentInterval(now time.Time) (time.Time, time.Time) {

	loc := now.Location()

	startOfDay := time.Date(
		now.Year(),
		now.Month(),
		now.Day(),
		9, 30, 0, 0,
		loc,
	)

	diff := now.Sub(startOfDay)

	intervalIndex := int(diff.Minutes()) / 5

	start := startOfDay.Add(time.Duration(intervalIndex*5) * time.Minute)
	end := start.Add(5 * time.Minute)

	return start, end
}

func getCurrentIntervalRange(now time.Time) (time.Time, time.Time) {
	
	loc:= now.Location()

	startOfDay := time.Date(
		now.Year(),
		now.Month(),
		now.Day(),
		9, 15, 0, 0,
		loc,
	)

	diff := now.Sub(startOfDay)

	intervalIndex := int(diff.Minutes()) / 5

	start := startOfDay.Add(time.Duration(intervalIndex*5) * time.Minute)
	end := now

	return start, end
}