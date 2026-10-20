# AGENTS.md — wingmanager

Open-source PMS for individual landlords (short-term + long-term rentals). Go + SQLite + htmx, single binary.

**`docs/PLAN.md` is the spec. Read it fully before every task.** Decisions live in `docs/decisions/`.

## Precedence over the global ~/.codex/AGENTS.md

This project deliberately differs from the global file in these points (approved by the project owner via PLAN.md). Everything else in the global file still applies.

1. **Tests are required.** Write tests as specified in PLAN §9: real SQLite (temp file + migrations), no DB mocks; only network is faked (`httptest`). Every milestone's "完成标志" must be backed by a test where possible.
2. **Authentication is a product feature, not a guardrail.** Implement login, bcrypt password hashing, session tokens stored as SHA-256 hashes (credential storage, not a checksum), and cross-origin protection exactly as PLAN describes.
3. **Runtime parameters are CLI flags** (`--addr`, `--data-dir`) with defaults shown in `--help` (PLAN §3). Business settings live in the `settings` table.

## How you work here

- Do **only** the milestone named in the task. Do not start the next one, do not add features outside PLAN.
- **Do not commit.** Leave changes in the working tree; the supervisor reviews and commits.
- Dependencies: only the whitelist in PLAN §3. Needing another one → stop and report, don't add it.
- If PLAN is ambiguous or seems wrong: pick the simplest option consistent with PLAN, mark it in code with `// PLAN-DEVIATION:` and list it in your report. Never silently diverge.
- Schema changes: new numbered file in `internal/db/migrations/`. Never edit a migration that has been committed.
- Each domain package owns its SQL (`queries.sql` in the package, sqlc multi-package). Only the owning package may contain write queries for its tables.
- Enums (statuses, kinds, channels, categories) are defined once in Go and must match the DB CHECK constraints; keep a test that compares them.
- Identifiers, code comments, commit-style text: English. Docs for the owner (PLAN, decisions): Chinese.
- Money: `int64` cents. Stay/lease dates: `YYYY-MM-DD` strings in DB, never timestamps.

## Commands (all must pass before you report done)

```
gofmt -l .                 # must print nothing
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@latest ./...
sqlc diff                  # generated code is up to date with queries
go test -race ./...
```

## Report format (your final message)

1. 完成了什么（对照里程碑的完成标志逐条）
2. 改动的文件
3. 验证：运行了哪些命令、结果
4. PLAN-DEVIATION 列表
5. 需要监督者决定的问题
