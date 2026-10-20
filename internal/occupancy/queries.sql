-- name: futureOccupants :many
SELECT b.id AS id, b.unit_id AS unit_id, 'booking' AS source, b.check_in AS starts_on,
       b.check_out AS ends_on, p.name AS property_name, u.name AS unit_name
FROM bookings b JOIN units u ON u.id = b.unit_id JOIN properties p ON p.id = u.property_id
WHERE b.status = 'confirmed' AND b.check_out > sqlc.arg(today)
UNION ALL
SELECT l.id, l.unit_id, 'lease' AS source, l.starts_on,
       coalesce(l.ends_on, '') AS ends_on, p.name AS property_name, u.name AS unit_name
FROM leases l JOIN units u ON u.id = l.unit_id JOIN properties p ON p.id = u.property_id
WHERE l.status = 'active' AND (l.ends_on IS NULL OR l.ends_on >= sqlc.arg(today))
ORDER BY unit_id, starts_on, id;
