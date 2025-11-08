package config

import (
	"errors"
	"strings"
	"time"
)

// ParseISOTime поддерживает форматы:
//
//	2025-11-08
//	2025-11-08T10:00
//	2025-11-08T10:00:00
//	2025-11-08T10:00:00Z
func ParseISOTime(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02",
	}
	s = strings.TrimSpace(s)
	var err error
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		} else {
			err = errors.Join(err)
		}
	}
	return time.Time{}, err
}
