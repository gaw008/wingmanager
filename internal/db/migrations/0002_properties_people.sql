CREATE TABLE properties (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL,
  address     TEXT NOT NULL DEFAULT '',
  notes       TEXT NOT NULL DEFAULT '',
  archived_at TEXT
);

CREATE TABLE units (
  id                INTEGER PRIMARY KEY,
  property_id       INTEGER NOT NULL REFERENCES properties(id),
  name              TEXT NOT NULL,
  ical_export_token TEXT NOT NULL UNIQUE,
  archived_at       TEXT,
  UNIQUE (property_id, name)
);

CREATE TABLE people (
  id    INTEGER PRIMARY KEY,
  name  TEXT NOT NULL,
  email TEXT NOT NULL DEFAULT '',
  phone TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT ''
);
