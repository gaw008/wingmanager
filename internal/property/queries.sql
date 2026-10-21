-- name: createProperty :one
INSERT INTO properties (name, address, notes) VALUES (?, ?, ?) RETURNING id;

-- name: getProperty :one
SELECT id, name, address, notes, CAST(COALESCE(archived_at, '') AS TEXT) AS archived_at
FROM properties WHERE id = ?;

-- name: listProperties :many
SELECT id, name, address, notes, CAST(COALESCE(archived_at, '') AS TEXT) AS archived_at
FROM properties WHERE archived_at IS NULL OR sqlc.arg(include_archived)
ORDER BY name, id;

-- name: updateProperty :execrows
UPDATE properties SET name = ?, address = ?, notes = ? WHERE id = ?;

-- name: setPropertyArchived :execrows
UPDATE properties SET archived_at = CASE WHEN CAST(sqlc.arg(archived) AS INTEGER) THEN COALESCE(archived_at, datetime('now')) END
WHERE id = sqlc.arg(id);

-- name: createUnit :one
INSERT INTO units (property_id, name, ical_export_token) VALUES (?, ?, ?) RETURNING id;

-- name: getUnit :one
SELECT id, property_id, name, ical_export_token, CAST(COALESCE(archived_at, '') AS TEXT) AS archived_at
FROM units WHERE id = ?;

-- name: listUnits :many
SELECT id, property_id, name, ical_export_token, CAST(COALESCE(archived_at, '') AS TEXT) AS archived_at
FROM units WHERE property_id = ? AND (archived_at IS NULL OR sqlc.arg(include_archived))
ORDER BY name, id;

-- name: renameUnit :execrows
UPDATE units SET name = ? WHERE id = ?;

-- name: countActiveUnits :one
SELECT count(*) FROM units WHERE property_id = ? AND archived_at IS NULL;

-- name: setUnitArchived :exec
UPDATE units SET archived_at = CASE WHEN CAST(sqlc.arg(archived) AS INTEGER) THEN COALESCE(archived_at, datetime('now')) END
WHERE id = sqlc.arg(id);

-- name: regenerateToken :one
UPDATE units SET ical_export_token = ? WHERE id = ? RETURNING ical_export_token;

-- name: vacantUnits :many
SELECT u.id, p.name AS property_name, u.name AS unit_name
FROM units u JOIN properties p ON p.id = u.property_id
WHERE u.archived_at IS NULL AND p.archived_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM leases l WHERE l.unit_id = u.id AND l.status = 'active'
      AND l.starts_on <= sqlc.arg(today) AND (l.ends_on IS NULL OR l.ends_on >= sqlc.arg(today)))
  AND NOT EXISTS (SELECT 1 FROM bookings b WHERE b.unit_id = u.id AND b.status = 'confirmed'
      AND b.check_in <= sqlc.arg(today) AND b.check_out > sqlc.arg(today))
ORDER BY p.name, p.id, u.name, u.id;
