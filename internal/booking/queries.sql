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

-- name: upsertImported :one
INSERT INTO bookings (unit_id, kind, status, check_in, check_out, channel, feed_id, external_uid)
VALUES (?, ?, 'confirmed', ?, ?, ?, ?, ?)
ON CONFLICT (feed_id, external_uid) DO UPDATE SET
kind = excluded.kind, status = excluded.status, check_in = excluded.check_in,
check_out = excluded.check_out, channel = excluded.channel, updated_at = datetime('now')
WHERE bookings.kind != excluded.kind OR bookings.status != excluded.status
 OR bookings.check_in != excluded.check_in OR bookings.check_out != excluded.check_out
 OR bookings.channel != excluded.channel
RETURNING id;

-- name: listFeedBookings :many
SELECT * FROM bookings WHERE feed_id = ? ORDER BY id;

-- name: detachFeed :exec
UPDATE bookings SET feed_id = NULL, external_uid = NULL, updated_at = datetime('now') WHERE feed_id = ?;

-- name: updateImportedOwnerFields :exec
UPDATE bookings SET guest_id = ?, notes = ?, updated_at = datetime('now') WHERE id = ?;
