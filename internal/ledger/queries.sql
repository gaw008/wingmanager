-- name: listEntries :many
SELECT * FROM ledger_entries WHERE lease_id = ? ORDER BY occurred_on, id;

-- name: getEntry :one
SELECT * FROM ledger_entries WHERE id = ?;

-- name: createEntry :one
INSERT INTO ledger_entries (property_id, lease_id, kind, category, amount_cents, occurred_on, memo)
SELECT u.property_id, l.id, ?, ?, ?, ?, ? FROM leases l JOIN units u ON u.id = l.unit_id WHERE l.id = ? RETURNING id;

-- name: postRent :exec
INSERT INTO ledger_entries (property_id, lease_id, kind, category, amount_cents, occurred_on, period)
SELECT u.property_id, l.id, 'charge', 'rent', ?, ?, ? FROM leases l JOIN units u ON u.id = l.unit_id WHERE l.id = ?
ON CONFLICT (lease_id, period) WHERE period IS NOT NULL DO NOTHING;

-- name: voidEntry :exec
UPDATE ledger_entries SET voided_at = datetime('now'), void_reason = ? WHERE id = ? AND voided_at IS NULL;

-- name: createPropertyEntry :one
INSERT INTO ledger_entries (property_id, booking_id, kind, category, amount_cents, occurred_on, memo)
SELECT p.id, sqlc.narg(booking_id), sqlc.arg(kind), sqlc.arg(category), sqlc.arg(amount_cents), sqlc.arg(occurred_on), sqlc.arg(memo)
FROM properties p WHERE p.id = sqlc.arg(property_id)
AND (sqlc.narg(booking_id) IS NULL OR EXISTS (
 SELECT 1 FROM bookings b JOIN units u ON u.id = b.unit_id
 WHERE b.id = sqlc.narg(booking_id) AND u.property_id = p.id AND b.kind = 'stay'
)) RETURNING id;

-- name: filteredEntries :many
SELECT sqlc.embed(e), p.name AS property_name FROM ledger_entries e JOIN properties p ON p.id = e.property_id
WHERE (sqlc.arg(property_id) = 0 OR e.property_id = sqlc.arg(property_id))
 AND e.occurred_on >= sqlc.arg(from_date) AND e.occurred_on <= sqlc.arg(to_date)
 AND (sqlc.arg(kind) = '' OR e.kind = sqlc.arg(kind))
ORDER BY e.occurred_on, e.id;

-- name: reportProperties :many
SELECT id, name FROM properties WHERE (sqlc.arg(property_id) = 0 OR id = sqlc.arg(property_id)) ORDER BY id;
