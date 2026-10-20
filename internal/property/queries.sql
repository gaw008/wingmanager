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
