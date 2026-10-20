-- name: createDocument :one
INSERT INTO documents (property_id, unit_id, lease_id, person_id, ledger_entry_id, filename, mime, size_bytes)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: getDocument :one
SELECT * FROM documents WHERE id = ?;

-- name: listDocuments :many
SELECT * FROM documents
WHERE property_id = sqlc.narg('property_id') OR unit_id = sqlc.narg('unit_id')
   OR lease_id = sqlc.narg('lease_id') OR person_id = sqlc.narg('person_id')
   OR ledger_entry_id = sqlc.narg('ledger_entry_id')
ORDER BY id DESC;

-- name: deleteDocument :exec
DELETE FROM documents WHERE id = ?;
