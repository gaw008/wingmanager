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

-- name: calendarUnits :many
SELECT u.id, u.property_id, u.name, p.name AS property_name,
       (SELECT count(*) FROM calendar_feeds f WHERE f.unit_id = u.id) AS feed_count
FROM units u JOIN properties p ON p.id = u.property_id
WHERE u.archived_at IS NULL AND p.archived_at IS NULL
  AND (sqlc.arg(property_id) = 0 OR p.id = sqlc.arg(property_id))
ORDER BY p.name, p.id, u.name, u.id;

-- name: calendarOccupants :many
SELECT b.id, b.unit_id, b.kind, b.check_in AS starts_on, b.check_out AS ends_on,
       b.channel, coalesce(g.name, '') AS guest_name, 0 AS rent_cents, b.notes
FROM bookings b LEFT JOIN people g ON g.id = b.guest_id
WHERE b.status = 'confirmed'
UNION ALL
SELECT l.id, l.unit_id, 'lease', l.starts_on, coalesce(l.ends_on, ''), '',
       coalesce((SELECT group_concat(name, ', ') FROM
         (SELECT p.name FROM lease_tenants lt JOIN people p ON p.id = lt.person_id
          WHERE lt.lease_id = l.id ORDER BY p.name, p.id)), ''), l.rent_cents, l.notes
FROM leases l WHERE l.status IN ('active', 'ended')
ORDER BY unit_id, starts_on, id;
