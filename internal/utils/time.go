package utils

import "time"

func ParseTime(timeStr string) (time.Time, error) {
	// parse time and return time.Time
	layout := "2006-01-02T15:04:05Z"
	t, err := time.Parse(layout, timeStr)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}
