-- name: getBooking :one
SELECT * FROM bookings WHERE id = ?;

-- name: listBookings :many
SELECT * FROM bookings ORDER BY check_in, id;

-- name: listOccupyingBookings :many
SELECT * FROM bookings WHERE unit_id = ? AND status = ? AND id != ? ORDER BY check_in, id;

-- name: createBooking :one
INSERT INTO bookings (unit_id, kind, status, check_in, check_out, channel, guest_id, notes)
VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id;

-- name: updateBooking :exec
UPDATE bookings SET unit_id = ?, kind = ?, check_in = ?, check_out = ?, channel = ?,
guest_id = ?, notes = ?, updated_at = datetime('now') WHERE id = ?;

-- name: setBookingStatus :exec
UPDATE bookings SET status = ?, updated_at = datetime('now') WHERE id = ?;

-- name: unitArchived :one
SELECT archived_at IS NOT NULL FROM units WHERE id = ?;
