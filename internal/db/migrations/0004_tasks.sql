CREATE TABLE tasks (
  id          INTEGER PRIMARY KEY,
  property_id INTEGER NOT NULL REFERENCES properties(id),
  unit_id     INTEGER REFERENCES units(id),
  booking_id  INTEGER REFERENCES bookings(id),
  kind        TEXT NOT NULL CHECK (kind IN ('cleaning','maintenance','other')),
  title       TEXT NOT NULL,
  due_on      TEXT,
  status      TEXT NOT NULL CHECK (status IN ('open','done','cancelled')),
  assignee    TEXT NOT NULL DEFAULT '',
  notes       TEXT NOT NULL DEFAULT '',
  done_at     TEXT
);
CREATE UNIQUE INDEX one_cleaning_per_booking
  ON tasks (booking_id) WHERE kind = 'cleaning';
