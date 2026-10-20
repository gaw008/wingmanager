CREATE TABLE calendar_feeds (
  id              INTEGER PRIMARY KEY,
  unit_id         INTEGER NOT NULL REFERENCES units(id),
  channel         TEXT NOT NULL CHECK (channel IN ('airbnb','vrbo','booking','other')),
  url             TEXT NOT NULL,
  sync_generation INTEGER NOT NULL DEFAULT 0,
  last_attempt_at TEXT,
  last_success_at TEXT,
  last_error      TEXT,
  UNIQUE (unit_id, url)
);

CREATE TABLE bookings (
  id           INTEGER PRIMARY KEY,
  unit_id      INTEGER NOT NULL REFERENCES units(id),
  kind         TEXT NOT NULL CHECK (kind IN ('stay','block')),
  status       TEXT NOT NULL CHECK (status IN ('confirmed','cancelled')),
  check_in     TEXT NOT NULL,
  check_out    TEXT NOT NULL,
  channel      TEXT NOT NULL CHECK (channel IN ('direct','airbnb','vrbo','booking','other')),
  guest_id     INTEGER REFERENCES people(id),
  feed_id      INTEGER REFERENCES calendar_feeds(id),
  external_uid TEXT,
  notes        TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at   TEXT NOT NULL DEFAULT (datetime('now')),
  CHECK (check_out > check_in),
  CHECK ((feed_id IS NULL) = (external_uid IS NULL)),
  UNIQUE (feed_id, external_uid)
);
