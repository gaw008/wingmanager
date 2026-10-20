-- name: getFeed :one
SELECT * FROM calendar_feeds WHERE id = ?;

-- name: listFeeds :many
SELECT * FROM calendar_feeds ORDER BY id;

-- name: addFeed :one
INSERT INTO calendar_feeds (unit_id, channel, url) VALUES (?, ?, ?) RETURNING id;

-- name: editFeed :exec
UPDATE calendar_feeds SET channel = ?, url = ?, sync_generation = sync_generation + 1,
last_attempt_at = NULL, last_success_at = NULL, last_error = NULL WHERE id = ?;

-- name: removeFeed :exec
DELETE FROM calendar_feeds WHERE id = ?;

-- name: syncSucceeded :exec
UPDATE calendar_feeds SET sync_generation = sync_generation + 1,
last_attempt_at = ?, last_success_at = ?, last_error = NULL WHERE id = ?;

-- name: syncFailed :exec
UPDATE calendar_feeds SET last_attempt_at = ?, last_error = ?
WHERE id = ? AND sync_generation = ?;

-- name: unitForExport :one
SELECT id FROM units WHERE ical_export_token = ?;

-- name: futureConfirmedCount :one
SELECT count(*) FROM bookings WHERE feed_id = ? AND status = 'confirmed' AND check_out >= ?;

-- name: statusFeeds :many
SELECT sqlc.embed(f), p.id AS property_id, p.name AS property_name, u.name AS unit_name
FROM calendar_feeds f JOIN units u ON u.id = f.unit_id JOIN properties p ON p.id = u.property_id
ORDER BY p.name, u.name, f.id;
