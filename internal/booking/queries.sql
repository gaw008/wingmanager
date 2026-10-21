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

-- name: listActiveLeases :many
SELECT id, starts_on, ends_on FROM leases WHERE unit_id = ? AND status = 'active';

-- name: turnovers :many
SELECT b.id, b.check_in, b.check_out, p.name AS property_name, u.name AS unit_name,
       coalesce(g.name, '') AS guest_name
FROM bookings b JOIN units u ON u.id = b.unit_id JOIN properties p ON p.id = u.property_id
LEFT JOIN people g ON g.id = b.guest_id
WHERE b.status = 'confirmed' AND b.kind = 'stay'
  AND (b.check_in BETWEEN sqlc.arg(today) AND sqlc.arg(through_date)
       OR b.check_out BETWEEN sqlc.arg(today) AND sqlc.arg(through_date))
ORDER BY b.check_in, b.id;

-- name: todayStays :many
SELECT b.id, b.check_in, b.check_out, b.channel, p.name AS property_name, u.name AS unit_name,
       coalesce(g.name, '') AS guest_name
FROM bookings b JOIN units u ON u.id = b.unit_id JOIN properties p ON p.id = u.property_id
LEFT JOIN people g ON g.id = b.guest_id
WHERE b.status = 'confirmed' AND b.kind = 'stay'
  AND b.check_out >= sqlc.arg(today) AND b.check_in <= sqlc.arg(through_date)
ORDER BY b.check_in, b.id;
