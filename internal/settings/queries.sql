-- name: get :one
SELECT landlord_name, timezone, currency, locale FROM settings WHERE id = 1;

-- name: save :exec
UPDATE settings SET landlord_name = ?, timezone = ?, currency = ?, locale = ? WHERE id = 1;
