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
