package web

import "testing"

func TestDisplayDate(t *testing.T) {
	for _, tt := range []struct{ value, zh, en string }{
		{"2026-10-21", "10月21日", "Oct 21"},
		{"2025-10-21", "2025年10月21日", "Oct 21, 2025"},
		{"2027-01-01", "2027年1月1日", "Jan 1, 2027"},
		{"2024-02-29", "2024年2月29日", "Feb 29, 2024"},
		{"2026-09", "2026年9月", "Sep 2026"},
		{"2025-12", "2025年12月", "Dec 2025"},
		{"", "", ""},
	} {
		for locale, want := range map[string]string{"zh-CN": tt.zh, "en": tt.en} {
			got, err := displayDate(locale, 2026, tt.value)
			if err != nil || got != want {
				t.Errorf("displayDate(%s,2026,%q)=%q,%v want %q", locale, tt.value, got, err, want)
			}
		}
	}
	for _, raw := range []string{"2026-02-29", "2026-13", "2026-1", "2026-10-1", "0000-01-01"} {
		if _, err := displayDate("en", 2026, raw); err == nil {
			t.Errorf("accepted invalid date %q", raw)
		}
	}
}

func TestRentDueDay(t *testing.T) {
	for _, tt := range []struct {
		day    int64
		en, zh string
	}{
		{1, "1st of each month", "每月 1 日"}, {2, "2nd of each month", "每月 2 日"}, {3, "3rd of each month", "每月 3 日"},
		{4, "4th of each month", "每月 4 日"}, {11, "11th of each month", "每月 11 日"}, {12, "12th of each month", "每月 12 日"},
		{13, "13th of each month", "每月 13 日"}, {21, "21st of each month", "每月 21 日"}, {22, "22nd of each month", "每月 22 日"},
		{23, "23rd of each month", "每月 23 日"}, {28, "28th of each month", "每月 28 日"},
	} {
		for locale, want := range map[string]string{"zh-CN": tt.zh, "en": tt.en} {
			if got := rentDueDay(locale, tt.day); got != want {
				t.Errorf("rentDueDay(%s,%d)=%q want %q", locale, tt.day, got, want)
			}
		}
	}
}
