-- name: createPerson :one
INSERT INTO people (name, email, phone, notes) VALUES (?, ?, ?, ?) RETURNING id;

-- name: getPerson :one
SELECT id, CAST(name AS TEXT) AS name, email, phone, notes FROM people WHERE id = ?;

-- name: listPeople :many
SELECT id, CAST(name AS TEXT) AS name, email, phone, notes FROM people
WHERE instr(lower(name), lower(CAST(sqlc.arg(search) AS TEXT))) > 0 ORDER BY name, id;

-- name: updatePerson :execrows
UPDATE people SET name = ?, email = ?, phone = ?, notes = ? WHERE id = ?;
