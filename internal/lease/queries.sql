-- name: getLease :one
SELECT * FROM leases WHERE id = ?;

-- name: listLeases :many
SELECT * FROM leases ORDER BY starts_on, id;

-- name: createLease :one
INSERT INTO leases (unit_id, status, starts_on, ends_on, rent_cents, due_day, deposit_cents, notes)
VALUES (?, 'draft', ?, ?, ?, ?, ?, ?) RETURNING id;

-- name: updateLease :exec
UPDATE leases SET unit_id = ?, starts_on = ?, ends_on = ?, rent_cents = ?, due_day = ?, deposit_cents = ?, notes = ? WHERE id = ?;

-- name: setStatus :exec
UPDATE leases SET status = ? WHERE id = ?;

-- name: endLease :exec
UPDATE leases SET status = 'ended', ends_on = ? WHERE id = ?;

-- name: autoEnd :exec
UPDATE leases SET status = 'ended' WHERE status = 'active' AND ends_on < ?;

-- name: addTenant :exec
INSERT INTO lease_tenants (lease_id, person_id) VALUES (?, ?) ON CONFLICT DO NOTHING;

-- name: removeTenant :exec
DELETE FROM lease_tenants WHERE lease_id = ? AND person_id = ?;

-- name: listTenants :many
SELECT p.* FROM people p JOIN lease_tenants t ON t.person_id = p.id WHERE t.lease_id = ? ORDER BY p.name, p.id;

-- name: unitAvailable :one
SELECT u.archived_at IS NULL AND p.archived_at IS NULL FROM units u JOIN properties p ON p.id = u.property_id WHERE u.id = ?;

-- name: occupyingBookings :many
SELECT check_in, check_out FROM bookings WHERE unit_id = ? AND status = 'confirmed';

-- name: occupyingLeases :many
SELECT * FROM leases WHERE unit_id = ? AND status = 'active' AND id != ?;

-- name: currentLeases :many
SELECT sqlc.embed(l), p.name AS property_name, u.name AS unit_name,
       cast(coalesce((SELECT group_concat(name, ', ') FROM
           (SELECT g.name FROM lease_tenants lt JOIN people g ON g.id = lt.person_id
            WHERE lt.lease_id = l.id ORDER BY g.name, g.id)), '') AS TEXT) AS tenant_names
FROM leases l JOIN units u ON u.id = l.unit_id JOIN properties p ON p.id = u.property_id
WHERE l.status = 'active' AND l.starts_on <= sqlc.arg(today)
  AND (l.ends_on IS NULL OR l.ends_on >= sqlc.arg(today))
ORDER BY p.name, p.id, u.name, u.id;
