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

-- name: getTask :one
SELECT * FROM tasks WHERE id = ?;

-- name: listTasks :many
SELECT * FROM tasks
WHERE (sqlc.arg(property_id) = 0 OR property_id = sqlc.arg(property_id))
 AND (sqlc.arg(status) = '' OR status = sqlc.arg(status))
 AND (sqlc.arg(kind) = '' OR kind = sqlc.arg(kind))
 AND (sqlc.arg(due_from) = '' OR due_on >= sqlc.arg(due_from))
 AND (sqlc.arg(due_until) = '' OR due_on < sqlc.arg(due_until))
ORDER BY due_on IS NULL, due_on, id;

-- name: createTask :one
INSERT INTO tasks (property_id, unit_id, kind, title, due_on, status, assignee, notes)
SELECT p.id, sqlc.narg(unit_id), sqlc.arg(kind), sqlc.arg(title), sqlc.narg(due_on), 'open', sqlc.arg(assignee), sqlc.arg(notes)
FROM properties p WHERE p.id = sqlc.arg(property_id)
 AND (sqlc.narg(unit_id) IS NULL OR EXISTS (SELECT 1 FROM units u WHERE u.id=sqlc.narg(unit_id) AND u.property_id=p.id))
RETURNING id;

-- name: updateTask :exec
UPDATE tasks SET title = ?, due_on = ?, assignee = ?, notes = ? WHERE id = ?;

-- name: setTaskStatus :exec
UPDATE tasks SET status = sqlc.arg(status),
 done_at = CASE WHEN sqlc.arg(status) = 'done' THEN COALESCE(done_at,datetime('now')) ELSE NULL END
WHERE id = sqlc.arg(id);
