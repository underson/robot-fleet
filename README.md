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
