CREATE TABLE documents (
  -- IDs name files permanently; reusing a deleted ID could race its disk cleanup.
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  property_id     INTEGER REFERENCES properties(id),
  unit_id         INTEGER REFERENCES units(id),
  lease_id        INTEGER REFERENCES leases(id),
  person_id       INTEGER REFERENCES people(id),
  ledger_entry_id INTEGER REFERENCES ledger_entries(id),
  filename        TEXT NOT NULL,
  mime            TEXT NOT NULL,
  size_bytes      INTEGER NOT NULL,
  uploaded_at     TEXT NOT NULL DEFAULT (datetime('now')),
  CHECK ((property_id IS NOT NULL) + (unit_id IS NOT NULL) +
         (lease_id IS NOT NULL) + (person_id IS NOT NULL) +
         (ledger_entry_id IS NOT NULL) = 1)
);
