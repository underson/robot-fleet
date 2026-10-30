# robot-fleet

A small fleet-telemetry system: a robot simulator, a Go backend that ingests and stores telemetry and
derives fleet health, and a React console for a fleet manager.

> Work in progress. Design and decisions: [`docs/DESIGN.md`](docs/DESIGN.md).

## Run it
_TODO: `docker compose up`, then open the console._

## What I built and why
_TODO_

## What I faked or left out, and why
_TODO_

## With more time / in production
_TODO_

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
