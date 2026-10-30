# robot-fleet

Fleet telemetry: robot simulator → Go backend (ingestion, storage, fleet health) → React console.
Design and decisions live in `docs/DESIGN.md`; it is the source of truth. Update it when a decision changes.

## Layout
- `backend/`: Go module. `cmd/fleetd` (API server), `cmd/simulator` (robot simulator).
  `internal/domain/{telemetry,fleet}` (business, owns its ports), `internal/adapters/{vda,httpapi,postgres}`
  (technology), `internal/config`. Full layout and reasons: `docs/DESIGN.md` §5.
- `web/`: React + TypeScript (Vite), TanStack Query; API types generated from the OpenAPI spec.
- `docker-compose.yml`: Postgres + backend + simulator + web; `docker compose up` runs the whole system.

## Architecture rules
- Hexagonal: domain packages import nothing from adapters (HTTP, Postgres, wire formats). Dependencies point inwards.
- Wire formats (the VDA 5050-inspired message) exist only in DTOs; translators turn them into domain types at the edge.
- Write side (ingestion) and read side (fleet queries) don't share code paths.
- Business rules (ordering, dedup, health) live in the domain, not in SQL or handlers. Thresholds come from `HealthPolicy`.

## Go conventions
- Standard library first (`net/http`, `log/slog`); pgx for Postgres; no web framework.
- Errors wrapped with context (`fmt.Errorf("...: %w", err)`); no panics outside `main`.
- Configuration from environment variables only.

## Tests
- Table-driven unit tests for domain rules; no database needed.
- Integration tests against real Postgres (Testcontainers) for storage behaviour (conditional upsert, dedup).
- Web: component tests for the views that carry rules (attention list, filters).

## How we work
- Propose before building: surface design choices and trade-offs instead of deciding silently.
- Small, focused commits, each with a message that explains why. Never one large commit.
- Every change must be small enough for a human to review in full before it is committed.
