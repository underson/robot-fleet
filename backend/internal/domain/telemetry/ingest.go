package telemetry

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type RobotID string

// Record is a report we accepted: tied to our RobotID and stamped with the
// server clock, which decides liveness.
type Record struct {
	RobotID    RobotID
	ReceivedAt time.Time
	Report
}

// Supersedes reports whether r should replace current as the robot's state.
// Ordering uses the robot clock; an equal timestamp is a duplicate, not newer.
func (r Record) Supersedes(current Record) bool {
	return r.ReportedAt.After(current.ReportedAt)
}

type Status string

const (
	StatusApplied   Status = "applied"   // stored and is now the robot's current state
	StatusLate      Status = "late"      // stored in history; a newer state already exists
	StatusDuplicate Status = "duplicate" // already stored; nothing changed
	StatusRejected  Status = "rejected"  // not stored, see Reason
)

type Reason string

const (
	ReasonInvalid      Reason = "invalid"
	ReasonUnknownRobot Reason = "unknown_robot"
	ReasonClockSkew    Reason = "clock_skew"
)

// Outcome is the result for one report, in the order the reports came in.
type Outcome struct {
	Status Status
	Reason Reason // only for StatusRejected
	Detail string
}

type Ingestor struct {
	registry     Registry
	history      HistoryStore
	state        StateStore
	now          func() time.Time
	maxClockSkew time.Duration
}

// NewIngestor wires the use case. maxClockSkew is how far ahead of the server
// clock a report may be stamped: a robot with a fast clock would otherwise win
// every ordering comparison and freeze its state.
func NewIngestor(registry Registry, history HistoryStore, state StateStore, now func() time.Time, maxClockSkew time.Duration) *Ingestor {
	return &Ingestor{registry: registry, history: history, state: state, now: now, maxClockSkew: maxClockSkew}
}

// Ingest accepts a batch and returns one outcome per report. Bad reports are
// rejected individually; an error means an infrastructure failure, and the
// whole batch can be retried safely because every step is idempotent.
func (in *Ingestor) Ingest(ctx context.Context, reports []Report) ([]Outcome, error) {
	receivedAt := in.now()
	outcomes := make([]Outcome, len(reports))
	robotIDs := make(map[Identity]RobotID)

	var records []Record
	var positions []int // index in reports of each record
	for i, rep := range reports {
		if err := rep.Validate(); err != nil {
			outcomes[i] = rejected(ReasonInvalid, err)
			continue
		}
		if rep.ReportedAt.After(receivedAt.Add(in.maxClockSkew)) {
			outcomes[i] = rejected(ReasonClockSkew, fmt.Errorf("reportedAt %s is ahead of server time %s", rep.ReportedAt.Format(time.RFC3339), receivedAt.Format(time.RFC3339)))
			continue
		}
		robotID, ok := robotIDs[rep.Robot]
		if !ok {
			var err error
			robotID, err = in.registry.Lookup(ctx, rep.Robot)
			if errors.Is(err, ErrUnknownRobot) {
				outcomes[i] = rejected(ReasonUnknownRobot, fmt.Errorf("%s %s is not registered", rep.Robot.Manufacturer, rep.Robot.SerialNumber))
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("look up robot: %w", err)
			}
			robotIDs[rep.Robot] = robotID
		}
		records = append(records, Record{RobotID: robotID, ReceivedAt: receivedAt, Report: rep})
		positions = append(positions, i)
	}
	if len(records) == 0 {
		return outcomes, nil
	}

	// History first, so the current state never points at something history lacks.
	inserted, err := in.history.Append(ctx, records)
	if err != nil {
		return nil, fmt.Errorf("append history: %w", err)
	}

	// Only the newest record per robot can become its state; older ones in
	// the same batch are late by definition.
	newest := make(map[RobotID]int) // RobotID -> index in records
	for j, rec := range records {
		if k, ok := newest[rec.RobotID]; !ok || rec.Supersedes(records[k]) {
			newest[rec.RobotID] = j
		}
	}
	saved := make(map[int]bool, len(newest))
	for _, j := range newest {
		ok, err := in.state.SaveIfNewer(ctx, records[j])
		if err != nil {
			return nil, fmt.Errorf("save state: %w", err)
		}
		saved[j] = ok
	}

	// A record counts as applied whenever it updated the state, even if
	// history already had it: that is a retry after a failed state write.
	for j, i := range positions {
		switch {
		case saved[j]:
			outcomes[i] = Outcome{Status: StatusApplied}
		case !inserted[j]:
			outcomes[i] = Outcome{Status: StatusDuplicate}
		default:
			outcomes[i] = Outcome{Status: StatusLate}
		}
	}
	return outcomes, nil
}

func rejected(reason Reason, err error) Outcome {
	return Outcome{Status: StatusRejected, Reason: reason, Detail: err.Error()}
}
