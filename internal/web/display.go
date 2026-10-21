package web

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/settings"
)

// displayDate formats canonical dates and rent periods for the UI. Only dates in
// currentYear omit the year; periods always include it. Empty means no date.
func displayDate(locale string, currentYear int, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	period := len(value) == len("2006-01")
	layout := time.DateOnly
	if period {
		layout = "2006-01"
	}
	date, err := time.Parse(layout, value)
	if err != nil || date.Year() < 1 || date.Format(layout) != value {
		return "", civildate.ErrDate
	}
	if locale == settings.LocaleChinese {
		if period {
			return fmt.Sprintf("%d年%d月", date.Year(), date.Month()), nil
		}
		label := fmt.Sprintf("%d月%d日", date.Month(), date.Day())
		if date.Year() != currentYear {
			label = fmt.Sprintf("%d年%s", date.Year(), label)
		}
		return label, nil
	}
	if period {
		return date.Format("Jan 2006"), nil
	}
	if date.Year() != currentYear {
		return date.Format("Jan 2, 2006"), nil
	}
	return date.Format("Jan 2"), nil
}

func rentDueDay(locale string, day int64) string {
	if locale == settings.LocaleChinese {
		return fmt.Sprintf("每月 %d 日", day)
	}
	suffix := "th"
	if day%100 < 11 || day%100 > 13 {
		switch day % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.FormatInt(day, 10) + suffix + " of each month"
}
