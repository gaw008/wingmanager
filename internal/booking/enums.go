package booking

const (
	KindStay  = "stay"
	KindBlock = "block"

	StatusConfirmed = "confirmed"
	StatusCancelled = "cancelled"

	ChannelDirect  = "direct"
	ChannelAirbnb  = "airbnb"
	ChannelVRBO    = "vrbo"
	ChannelBooking = "booking"
	ChannelOther   = "other"
)

// Kinds returns the supported manual booking kinds.
func Kinds() []string { return []string{KindStay, KindBlock} }

// Statuses returns the persisted booking states.
func Statuses() []string { return []string{StatusConfirmed, StatusCancelled} }

// Channels includes channels whose bookings the owner records manually.
func Channels() []string {
	return []string{ChannelDirect, ChannelAirbnb, ChannelVRBO, ChannelBooking, ChannelOther}
}
