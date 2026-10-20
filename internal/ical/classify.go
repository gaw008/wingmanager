package ical

import (
	"github.com/gaw008/wingmanager/internal/booking"
	"strings"
)

// classify centralizes the platform-specific distinction between stays and blocks.
func classify(channel, summary string) string {
	switch channel {
	case booking.ChannelAirbnb:
		if summary == "Airbnb (Not available)" {
			return booking.KindBlock
		}
	case booking.ChannelVRBO:
		if summary == "Blocked" {
			return booking.KindBlock
		}
	case booking.ChannelBooking:
		return booking.KindStay
	default:
		value := strings.ToLower(summary)
		for _, word := range []string{"block", "not available", "unavailable", "closed"} {
			if strings.Contains(value, word) {
				return booking.KindBlock
			}
		}
	}
	return booking.KindStay
}
