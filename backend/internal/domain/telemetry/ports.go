package telemetry

import (
	"context"
	"errors"
)

// ErrUnknownRobot is returned by a Registry for an identity nobody registered.
var ErrUnknownRobot = errors.New("unknown robot")

// Registry maps a robot's own identity to our RobotID.
type Registry interface {
	Lookup(ctx context.Context, id Identity) (RobotID, error)
}

// HistoryStore keeps every accepted record.
type HistoryStore interface {
	// Append stores the records and reports, per record, whether it was new.
	// A record with the same robot and ReportedAt as a stored one is a
	// duplicate: it is skipped, not an error.
	Append(ctx context.Context, records []Record) (inserted []bool, err error)
}

// StateStore keeps the current state of each robot.
type StateStore interface {
	// SaveIfNewer replaces the robot's current state only if rec supersedes it
	// (see Record.Supersedes), and reports whether it did. Implementations
	// must make the check and the write atomic.
	SaveIfNewer(ctx context.Context, rec Record) (saved bool, err error)
}
