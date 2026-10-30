# robot-fleet

A small fleet-telemetry system: a **robot simulator** sends state about once per second, **fleetd** (Go +
Postgres) ingests it, keeps each robot's current state and history and derives fleet health, and a **web
console** (React + TypeScript) shows a fleet manager what needs attention right now and one robot in detail.

Design and the reasoning behind each decision: [`docs/DESIGN.md`](docs/DESIGN.md).

## Run it
Needs Docker with Compose.
```
docker compose up --build
```
- **Console:** http://localhost:3000
- **API:** http://localhost:8080 (contract: [`/openapi.yaml`](backend/internal/adapters/httpapi/openapi.yaml),
  rejection counters: `/debug/vars`)

fleetd migrates the database and loads demo data (3 sites, 24 robots) on start; the simulator waits for it, then
drives every robot. Within about 30 seconds the console shows the scripted problems at the top:

| Robot | Scripted behaviour | What the console shows |
|---|---|---|
| ACME-0001 | emergency stop, 40 s every 90 s | Emergency stop |
| ACME-0002 | critical motor fault, 30 s every 75 s | Severe fault |
| ACME-0003 | stops reporting, 40 s every 2 min | Offline (after 10 s of silence) |
| ACME-0004 | drains and never charges | Battery critical |
| ACME-0005 | nearly empty but on its charger | Critical battery, **not** flagged: it's charging |
| ACME-0006 | loses localization, 20 s every minute | Position unknown on the detail page |

The simulator also sends duplicates, late reports, a robot nobody registered and a clock an hour ahead; its log
prints a running count per outcome. Stop with `docker compose down -v` (`-v` drops the database).

### Development
```
cd backend && go test ./...              # unit + Postgres integration tests (Testcontainers, needs Docker)
cd backend && go test -short ./...       # unit tests only
cd web && npm ci && npm run dev          # console on :5173, proxies /v1 to fleetd on :8080
cd web && npm run gen:api                # regenerate API types after changing openapi.yaml
```
With **Colima** instead of Docker Desktop, Testcontainers needs:
`DOCKER_HOST=unix://$HOME/.colima/default/docker.sock TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock`.
CI (GitHub Actions) runs gofmt, vet and all Go tests with `-race`, checks the UI's generated types still match
`openapi.yaml`, then typechecks, tests and builds the web app.

## What I built and why
All three parts, with the depth where the problem seems harder: **turning an unreliable stream of reports into a
correct picture of the fleet.** The simulator and UI exist to make that visible.

**Domain model** ([DESIGN §4](docs/DESIGN.md#4-domain-model))
- **Two clocks.** The robot's timestamp orders reports; the server's receive time decides liveness. A robot with a
  wrong clock can't make itself look online, and reports more than 5 s in the future are rejected (`clock_skew`).
- **Every report gets an outcome:** `applied`, `late` (older than the current state, kept in history only),
  `duplicate`, or `rejected` with a reason (`invalid`, `unknown_robot`, `clock_skew`). One bad message never fails
  the batch, and the gateway learns what happened to each one.
- **Health is derived on read** by a `HealthPolicy` (offline after 10 s, battery low/critical thresholds), never
  stored, so it can't go stale and thresholds can change without migrating data. Attention reasons are ranked:
  emergency stop, severe fault, offline, critical battery (only when not charging).
- **Unknown robots are rejected**, logged and counted, not auto-registered: accepting anything that posts would let
  any device write into a customer's fleet.

**Architecture** ([DESIGN §5](docs/DESIGN.md#5-architecture))
- **Hexagonal modular monolith.** `internal/domain` (telemetry, fleet) imports nothing from `internal/adapters`
  (vda, postgres, httpapi). Each domain package defines the ports it needs; adapters implement them.
- **Write and read sides are separate** (light CQRS, one database): ingestion protects per-robot invariants, the
  fleet side works across robots. Mostly a modelling choice today; it keeps the scaling steps local later.
- **Anti-corruption layer.** The wire message is [VDA 5050](https://github.com/VDA5050/VDA5050)-inspired (the
  industry's robot-state format), but its names exist only in `adapters/vda`. Another vendor format or MQTT
  gateway is a new translator, not a domain change.
- **Contract first.** `openapi.yaml` is the API contract; the UI's types are generated from it and CI fails if
  they drift.

**Storage** ([DESIGN §6](docs/DESIGN.md#6-storage-postgres))
- Postgres: a relational registry, an append-only history keyed `(robot_id, reported_at)` (that key is the dedup
  rule), and one current-state row per robot written by a **conditional upsert**: the database itself refuses an
  older report, so ordering holds even with concurrent writers. Integration tests run it against real Postgres.
- History before state, each step idempotent, so a retried batch after a crash ends in the same state without a
  transaction across both writes.

**Simulator and UI** (deliberately small; see "How I used AI")
- The simulator acts as a gateway: discovers robots through the API, sends one batch per second, scripted roles
  plus network faults, fixed seed for reproducible runs.
- The console: summary tiles, site and status filters in the URL, an attention-first table, and a robot page with
  state, faults and battery over time. It polls every 2 s and renders the API's order: urgency is a domain rule and
  lives once, on the server.

```
backend/cmd/{fleetd,simulator}     binaries: wiring only / the simulator
backend/internal/domain/           telemetry (write side), fleet (read side): rules, use cases, ports
backend/internal/adapters/         vda (wire format), httpapi (+ openapi.yaml), postgres (+ migrations, seed)
backend/internal/config/           environment configuration
web/src/{api,features,shared}      generated types + query hooks, fleet and robot pages, shared labels
docs/DESIGN.md                     problem, model, architecture, storage, 100×, on-prem, decisions
```

## What I faked or left out, and why
- **Transport is HTTP batches**, not MQTT. Robots and VDA 5050 usually speak MQTT through a gateway; HTTP keeps the
  demo simple, and MQTT would be another adapter feeding the same ingestion use case.
- **The robot registry is seed data.** Provisioning robots belongs with identity and access management; there is
  no API to register robots.
- **No authentication** on the API or console, and no multi-tenancy. Both are needed before a real deployment.
- **No alerting.** Rejections are logged and counted per reason; turning that into an alert (and an "unregistered
  devices" list) is described in DESIGN §6.
- **Polling, not push.** Correct by construction because health is derived from the server clock on each read;
  SSE is the next step for many consoles (§7).
- **History is not partitioned or downsampled.** Fine for a demo; at volume it's the first table to change (§7).
- **The UI is one page of up to 200 robots**, no table paging (the API pages), no map, no chart library, and only
  a few component tests. Robot position is shown as coordinates.
- **The simulator doesn't buffer and retry** when fleetd is down; it sends fresh state on the next tick. A real
  gateway would, and that buffering is also where most late reports come from.
- **Not VDA 5050 compliant**: the message borrows its structure and names, not the full schema.

## With more time / in production
Scale and on-prem are written up in [DESIGN §7 (100×)](docs/DESIGN.md#7-at-100-written-not-built) and
[§8 (on-prem)](docs/DESIGN.md#8-on-prem). In short:
- **At 100×:** keep current state in memory in the ingestion service and flush meaningful changes; put a queue
  (Kafka or NATS) between the edge and the writers; partition and downsample history, and move it to TimescaleDB if
  Postgres runs out (it sits behind the `HistoryStore` port); compute offline as an event, not a scan; SSE to the UI.
- **Operations:** Prometheus `/metrics` with latency histograms instead of `expvar`, and `/debug/*` on a separate
  admin port on the internal network; ingestion latency, queue lag and rejection-rate alerts.
- **Security:** authenticated gateways (mTLS or per-gateway tokens), users and roles for the console, tenants.
- **Product:** an unregistered-devices list with one-click registration, alert rules per site, a floor-plan map,
  event history per robot (e-stop at 10:42, fault cleared at 10:45).
- **Gateway:** MQTT adapter, buffering and retry with backoff.

## How I used AI
I worked with Claude Code as a pair programmer, not as an author:
- **I made the design decisions** (domain model, boundaries, storage, scope) and wrote them down in
  `docs/DESIGN.md` before writing code. The AI proposed options and pushed back; I chose.
- **Small steps, reviewed.** Each change is small enough to read in full. I reviewed every line before
  committing and rewrote what I didn't agree with or couldn't explain.
- **Tests as the check.** Domain rules are covered by unit tests and storage by integration tests against a real
  Postgres, so AI-written code had to pass checks I defined.
- **Explicit rules for the assistant.** [`CLAUDE.md`](CLAUDE.md) holds the architecture and testing rules I gave it,
  so it followed the project's conventions instead of improvising.
- The commit history shows the order of decisions: design first, then domain, then adapters, then the UI.

**Where I spent my own attention.** I focused my design and review time on **fleetd**: the domain model,
ingestion rules, storage and API. Every decision there is mine, and I reviewed it line by line. The
**simulator** and especially the **UI** were more AI-driven: I set the scope and the structure (types generated
from the OpenAPI contract, one query hook per endpoint, feature folders, filters in the URL) and reviewed the
result, but I didn't hand-craft the frontend details. They exist to make the backend's behaviour visible, so I
kept them deliberately small.
