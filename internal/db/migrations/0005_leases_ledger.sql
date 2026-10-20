CREATE TABLE leases (
  id            INTEGER PRIMARY KEY,
  unit_id       INTEGER NOT NULL REFERENCES units(id),
  status        TEXT NOT NULL CHECK (status IN ('draft','active','ended')),
  starts_on     TEXT NOT NULL,
  ends_on       TEXT,
  rent_cents    INTEGER NOT NULL CHECK (rent_cents > 0),
  due_day       INTEGER NOT NULL CHECK (due_day BETWEEN 1 AND 28),
  deposit_cents INTEGER NOT NULL DEFAULT 0 CHECK (deposit_cents >= 0),
  notes         TEXT NOT NULL DEFAULT '',
  CHECK (ends_on IS NULL OR ends_on >= starts_on)
);

CREATE TABLE lease_tenants (
  lease_id  INTEGER NOT NULL REFERENCES leases(id),
  person_id INTEGER NOT NULL REFERENCES people(id),
  PRIMARY KEY (lease_id, person_id)
);


CREATE TABLE ledger_entries (
  id           INTEGER PRIMARY KEY,
  property_id  INTEGER NOT NULL REFERENCES properties(id),
  lease_id     INTEGER REFERENCES leases(id),
  booking_id   INTEGER REFERENCES bookings(id),
  kind         TEXT NOT NULL CHECK (kind IN
                 ('charge','credit','payment','income','expense','deposit_in','deposit_out')),
  category     TEXT NOT NULL,
  amount_cents INTEGER NOT NULL CHECK (amount_cents > 0),
  occurred_on  TEXT NOT NULL,
  period       TEXT,
  memo         TEXT NOT NULL DEFAULT '',
  voided_at    TEXT,
  void_reason  TEXT,
  created_at   TEXT NOT NULL DEFAULT (datetime('now')),
  CHECK (kind NOT IN ('charge','credit','payment','deposit_in','deposit_out') OR lease_id IS NOT NULL),
  CHECK (period IS NULL OR (kind = 'charge' AND category = 'rent'))
);

CREATE UNIQUE INDEX one_rent_charge_per_period
  ON ledger_entries (lease_id, period) WHERE period IS NOT NULL;

