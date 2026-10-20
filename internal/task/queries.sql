-- name: ensureCleaning :exec
INSERT INTO tasks (property_id, unit_id, booking_id, kind, title, due_on, status)
SELECT u.property_id, b.unit_id, b.id, 'cleaning', '', b.check_out, sqlc.arg(status)
FROM bookings b JOIN units u ON u.id = b.unit_id WHERE b.id = sqlc.arg(booking_id)
ON CONFLICT (booking_id) WHERE kind = 'cleaning' DO UPDATE SET
property_id = excluded.property_id, unit_id = excluded.unit_id,
due_on = excluded.due_on, status = excluded.status
WHERE tasks.status != 'done' AND
(tasks.property_id != excluded.property_id OR tasks.unit_id != excluded.unit_id
 OR tasks.due_on != excluded.due_on OR tasks.status != excluded.status);

-- name: cancelCleaning :exec
UPDATE tasks SET status = sqlc.arg(status)
WHERE booking_id = sqlc.arg(booking_id) AND kind = 'cleaning' AND status = 'open';

-- name: cleaningForBooking :one
SELECT * FROM tasks WHERE booking_id = ? AND kind = 'cleaning';
