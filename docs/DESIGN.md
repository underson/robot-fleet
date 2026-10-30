# Design: fleet telemetry

## 1. Problem
Robots at different sites report their state about once per second (battery, position, mode, errors).
A fleet manager needs to know **what the fleet is doing right now** and to look at **one robot in detail**.

## 2. What a fleet manager actually needs
1. **Fleet now:** every robot's current state, filterable by site and status, with counts ("12 online, 2 offline, 3 need attention").
2. **What needs attention:** robots that are offline, in emergency stop, reporting a serious error or about to run
   out of battery, listed first.
3. **One robot:** current state plus recent history (battery over time, last positions, recent errors).

Deliberately out of scope: commanding robots (orders, actions), maps/floor plans, auth (IAM), alerts/notifications,
multi-tenancy.

## 3. Telemetry message (inspired by VDA 5050)
[VDA 5050](https://github.com/VDA5050/VDA5050) (3.0, MIT-licensed) is the open standard for communication between
mobile robots and fleet control. It is built for AGVs in intralogistics, and most of it is about executing orders
(nodes, edges, actions). This system **borrows its state concepts, not the whole spec**: identity, position, power,
operating mode, errors and safety. It does not claim compliance.

```json
{
  "manufacturer": "acme",
  "serialNumber": "CLN-0042",
  "timestamp": "2026-10-30T09:15:02.120Z",
  "position": { "mapId": "hq-floor-2", "x": 12.4, "y": 3.1, "theta": 1.57, "localized": true },
  "powerSupply": { "stateOfCharge": 18.5, "charging": false },
  "operatingMode": "AUTOMATIC",
  "errors": [{ "errorType": "bumperPressed", "errorLevel": "WARNING", "errorDescription": "Front bumper contact" }],
  "safetyState": { "activeEmergencyStop": "NONE", "fieldViolation": false },
  "vendorData": { "brushWear": 0.34 }
}
```

- `operatingMode`: `STARTUP`, `AUTOMATIC`, `SEMIAUTOMATIC`, `INTERVENED`, `MANUAL`, `SERVICE`, `TEACH_IN` (as in VDA 5050).
- `errorLevel`: `WARNING`, `URGENT`, `CRITICAL`, `FATAL` (as in VDA 5050).
- `activeEmergencyStop`: `NONE`, `MANUAL`, `REMOTE`.
- `vendorData`: anything manufacturer-specific; stored, not interpreted.
- Left out from VDA 5050: `headerId`, `version`, velocity, loads, order/node/edge and action states, the
  `connection`, `visualization` and `factsheet` topics.

## 4. Domain model
- **Site**: where robots operate (id, name).
- **Robot**: a registered machine: our `RobotID`, its **identity** (`manufacturer` + `serialNumber`, unique),
  site, model, display name. Registry data, changes rarely.
- **TelemetryReport**: one accepted message in domain terms: `RobotID`, `ReportedAt` (robot clock), `Position`
  (map coordinates in metres + `theta`; indoor robots use building maps, not GPS), `Power` (state of charge %,
  charging), `Mode`, `Errors` (type, level, description), `Safety` (emergency stop, field violation), `VendorData`.
- **RobotState**: the latest accepted report for a robot, plus `ReceivedAt` (server clock).
- **Health** (derived on read, never stored), using a `HealthPolicy`:
  - `connectivity`: online / offline
  - `battery`: ok / low / critical
  - `position`: known / unknown (`localized: false` means x, y, theta can't be trusted)
  - `needsAttention` with the reasons, when any of:
    - offline
    - emergency stop active
    - an error at `CRITICAL` or `FATAL` level
    - battery critical **and not charging** (a low robot on its charger is fine)

### Rules
- **Ordering uses the robot clock, liveness uses the server clock.** A report older than the stored
  `ReportedAt` is kept in history but never overwrites the current state. A robot is offline when no report
  has been *received* for N seconds (robot clocks drift, so `ReportedAt` can't decide liveness).
  VDA 5050 makes the same point: fleet control shouldn't judge robot health from the connection topic, and robots
  must send state only "at least every 30 seconds", so N is configurable.
- **Duplicates** (same robot, same `ReportedAt`) are ignored, so robots and gateways can retry safely.
- **Thresholds** (offline after N s, low/critical battery %) live in `HealthPolicy`, not in queries.

## 5. Architecture
Modular monolith, hexagonal: domain in the middle, adapters at the edges.

```
                 ┌─ ingestion (write side) ─────────────────────────────┐
simulator ─HTTP─▶│ wire DTO ─▶ translator (ACL) ─▶ telemetry domain     │─▶ StateStore / HistoryStore ─▶ Postgres
                 └───────────────────── Registry (identity → RobotID) ──┘                                  ▲
web (React) ◀─HTTP── query API ◀── fleet domain (HealthPolicy, overview, detail) ◀── FleetReader ───────────┘
```

### Code layout
```
backend/
  cmd/
    fleetd/            wiring only: config, adapters, start the server
    simulator/         robot simulator
  internal/
    domain/            the business; imports nothing from adapters
      telemetry/       write side: TelemetryReport, ordering/dedup rules, Ingest use case,
                       ports (StateStore, HistoryStore, Registry)
      fleet/           read side: HealthPolicy, attention reasons, overview and detail use cases, FleetReader port
    adapters/          the technology; implements or drives the domain's ports
      vda/             wire DTO + translator into telemetry types (the anti-corruption layer)
      httpapi/         HTTP handlers, request/response mapping, OpenAPI spec
      postgres/        implements telemetry's and fleet's ports; migrations
    config/            environment configuration, used by cmd/
```
- **`domain/` + `adapters/` is the hexagon in the folder tree.** Domain packages are named after the business,
  adapters after the technology. Dependencies point inwards: `adapters/postgres` imports `domain/telemetry`, never
  the reverse.
- **Each domain package owns its ports** (interfaces defined where they are used, Go-style); adapters implement them.
- **Use cases live in their domain package.** At this size a separate application layer would only add indirection;
  it's the first thing to split out if the use cases grow orchestration (transactions across contexts, events).
- **`vda/` is its own adapter**, so supporting another vendor format means adding a sibling package.
- Rejected: layer folders per context (`telemetry/domain`, `telemetry/adapters/postgres`), common in Java-style
  hexagonal projects but deep for Go at this size.

- **Ingestion** (write side): decode, translate, check identity, validate, apply ordering/dedup rules, persist.
- **Fleet** (read side): build the overview and robot detail, deriving health with `HealthPolicy`.
- Write and read sides share Postgres but not code paths (light CQRS, not full CQRS: one database, no events, no separate read store).

### Anti-corruption layer: wire format ≠ domain
VDA-style field names exist **only in the wire DTO**. A translator turns a message into domain types at the edge
of ingestion; the domain never sees `stateOfCharge` or `activeEmergencyStop`.
- Supporting a full VDA 5050 robot, an MQTT gateway or a manufacturer's own format means **adding a translator**,
  not changing the domain. That is the vendor-neutral platform in code.
- The translator owns format concerns (names, units, enums); the domain owns business rules (ordering, health).

### Why split write and read
Mainly **modelling**, with performance as an option it keeps open, not a gain today.
- **Different models.** The write side protects invariants per robot (ordering, dedup, known robot) and
  handles many tiny writes. The read side works across robots (filters, counts, derived health, history
  ranges). One model serving both ends up as a bloated `Robot` with everything on it.
- **Different reasons to change.** A new vendor format touches ingestion only; a new console view touches
  queries only.
- **Performance, later.** Today both sides hit the same Postgres, so there is no speed gain. The split makes
  the next steps local changes: batch/queue writes without touching queries; serve reads from a replica or
  cache; run ingestion as its own service with its own scaling. The write path is the hot one
  (about 1 msg/s per robot), the read path is a few managers polling, so they will scale very differently.
- **Not doing:** separate read databases, event sourcing, a message bus. Too much for this size; listed in §7 as triggers.

## 6. Storage (Postgres)
- `sites`, `robots`: registry, foreign keys; unique `(manufacturer, serial_number)`.
- `robot_state`: one row per robot, conditional upsert (`WHERE excluded.reported_at > robot_state.reported_at`).
- `telemetry`: append-only history, unique `(robot_id, reported_at)`, partitioned by time (described; maybe not built).
  Errors and vendor data as JSONB.
- Why Postgres over Mongo: integrity for registry data, atomic conditional upsert, SQL for fleet aggregates,
  JSONB for vendor-specific fields. History is the first candidate to move to a time-series store; the port makes that an adapter swap.

### Unknown robots
Telemetry whose `manufacturer` + `serialNumber` isn't in the registry is **rejected**: robots are registered on
purpose (provisioning belongs with IAM), and accepting anything that posts would let any device write into a customer's fleet.
- **Per-report result:** ingestion takes a batch, so the response says how many reports were accepted and
  which were rejected and why (`unknown_robot`, `invalid`), instead of failing the whole batch.
- **Visibility (built):** each rejection is logged as a structured event and counted per reason.
- **Alerting (described):** the counter feeds a metric; an alert fires when an unknown robot keeps reporting
  (e.g. > N rejections in 5 min). Usually this means a robot was installed but not registered, or a wrong
  serial/config, rarely something hostile. Next step: an "unregistered devices" list (manufacturer, serial,
  source, first and last seen, count) so an operator can register the robot with one click instead of reading logs.

## 7. At 100× (written, not built)
Today: thousands of robots ≈ thousands of reports/s. Target: 100k robots ≈ **100k reports/s, about 8.6 billion
history rows a day** at full resolution. What breaks first, and what changes:

| Breaks first | Why | Change |
|---|---|---|
| `robot_state` upserts | Postgres writes a new row version on every update; 100k updates/s on a 100k-row table means constant vacuum work | Keep latest state **in memory** (or Redis) in the ingestion service; flush to Postgres every few seconds or when something meaningful changes (mode, errors, e-stop, battery threshold crossed) |
| History inserts | One primary, WAL volume, index upkeep | **Batch** writes (`COPY`), **partition by time**, keep full resolution for hours and **downsample** to 1-minute aggregates after that (about 60× fewer rows); drop old partitions instead of `DELETE` |
| Bursts and outages | A gateway reconnecting after an outage sends a backlog at once | A **queue** (Kafka or NATS) between the HTTP/MQTT edge and the writers: absorbs bursts, allows replay, and decouples accepting a report from storing it |
| One ingestion process | CPU for decoding/validation | Ingestion becomes **its own service**, scaled horizontally, partitioned by robot so per-robot ordering still holds |
| Fleet reads | Large fleets × managers polling every 2 s | Read from a **replica** or the in-memory state; **SSE** pushes changes instead of polling; summary counts maintained incrementally instead of `COUNT(*)` on each request |
| Liveness | "Offline" computed by scanning all robots | A timer/heap per robot in the ingestion service emits an offline *event*; the read side stores it |

**Database choice at that scale.** For pure write volume, Mongo time-series collections or TimescaleDB beat plain
Postgres, and Mongo shards writes across nodes. But with the changes above, only the history store is under heavy
write load, and it sits behind the `HistoryStore` port. When it outgrows Postgres, the options are **TimescaleDB**
(still Postgres: same SQL, backups and on-prem install) or **Mongo time-series** (already in Olivaw's stack).
The registry and fleet views stay relational.

**What I'd measure before changing anything:** ingestion latency (received → stored), rejected reports per
reason, queue lag, upsert/insert rates, dead tuples and autovacuum on `robot_state`, API p95 per endpoint.

## 8. On-prem
Goal: the **same artefacts** run in Olivaw's cloud and on a customer's servers; only configuration differs.
- **Packaging:** one Go binary per service in a small container image; the web console is static files. For a
  customer: Docker Compose for a single server, a Helm chart for their Kubernetes.
- **Dependencies:** Postgres only. No AWS-specific services in the code path; anything cloud-specific (managed
  queue, object storage, secrets) sits behind a port with a self-hostable adapter (e.g. NATS instead of a managed queue).
- **Configuration:** environment variables only (database URL, `HealthPolicy` thresholds, ports); no config
  baked into images. Schema migrations run on startup or as a job, and are forward-compatible so customers
  can upgrade one version at a time.
- **Operations without our access:** structured JSON logs, a `/healthz` and `/readyz`, metrics endpoint; a
  support bundle (logs + config without secrets) a customer can send us. Offline installs need images in a
  customer registry or as a tarball.
- **Hybrid later:** an on-prem install that forwards aggregates (not raw telemetry) to the cloud, so the
  customer keeps raw data local and Olivaw still gets fleet-wide analytics.
- **Built in this repo:** env-only config, Postgres as the only dependency, `docker compose up`. Helm, air-gapped
  installs and upgrades across versions are described, not built.

## 9. Decisions
- Message format: **inspired by VDA 5050**, translated at the edge (§3, §5).
- Robot identity: **manufacturer + serial number**, mapped to our own `RobotID` by the registry.
- Unknown robot: **reject**, report per item, log + count (§6).
- Transport: **HTTP batch** for the take-home; MQTT discussed in the README (what robots/gateways often speak,
  and what VDA 5050 uses).
- Live updates in the UI: **polling**; SSE if time allows.
- Web console: **React + TypeScript (Vite)**, **TanStack Query** for fetching, caching and polling; API types
  generated from the OpenAPI spec, which is the contract between backend and UI.
- Database: **Postgres** for everything today; history is the first thing to move if write volume demands it (§7).
