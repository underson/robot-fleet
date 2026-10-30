package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"
)

var serverNow = time.Date(2026, 10, 30, 9, 15, 5, 0, time.UTC)

const knownRobot RobotID = "robot-1"

// fakeRegistry knows one robot: the identity used by validReport.
type fakeRegistry struct{}

func (fakeRegistry) Lookup(_ context.Context, id Identity) (RobotID, error) {
	if id == (Identity{Manufacturer: "acme", SerialNumber: "CLN-0042"}) {
		return knownRobot, nil
	}
	return "", ErrUnknownRobot
}

type historyKey struct {
	robot RobotID
	at    time.Time
}

type fakeHistory struct {
	records map[historyKey]Record
	err     error
}

func (h *fakeHistory) Append(_ context.Context, records []Record) ([]bool, error) {
	if h.err != nil {
		return nil, h.err
	}
	inserted := make([]bool, len(records))
	for recIdx, rec := range records {
		key := historyKey{rec.RobotID, rec.ReportedAt}
		if _, exists := h.records[key]; !exists {
			h.records[key] = rec
			inserted[recIdx] = true
		}
	}
	return inserted, nil
}

type fakeState struct {
	current map[RobotID]Record
	err     error
}

func (s *fakeState) SaveIfNewer(_ context.Context, rec Record) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	if current, exists := s.current[rec.RobotID]; exists && !rec.Supersedes(current) {
		return false, nil
	}
	s.current[rec.RobotID] = rec
	return true, nil
}

type testFixture struct {
	ingestor *Ingestor
	history  *fakeHistory
	state    *fakeState
}

func newFixture() testFixture {
	history := &fakeHistory{records: map[historyKey]Record{}}
	state := &fakeState{current: map[RobotID]Record{}}
	return testFixture{
		ingestor: NewIngestor(fakeRegistry{}, history, state, func() time.Time { return serverNow }, 5*time.Second),
		history:  history,
		state:    state,
	}
}

func reportAt(at time.Time) Report {
	report := validReport()
	report.ReportedAt = at
	return report
}

func ingest(t *testing.T, fixture testFixture, reports ...Report) []Outcome {
	t.Helper()
	outcomes, err := fixture.ingestor.Ingest(context.Background(), reports)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return outcomes
}

func assertOutcome(t *testing.T, got Outcome, status Status, reason Reason) {
	t.Helper()
	if got.Status != status || got.Reason != reason {
		t.Fatalf("got %+v, want status %q reason %q", got, status, reason)
	}
}

func TestIngestRejections(t *testing.T) {
	unknown := validReport()
	unknown.Robot.SerialNumber = "NOT-REGISTERED"
	invalid := validReport()
	invalid.Power.StateOfCharge = 140

	tests := []struct {
		name   string
		report Report
		reason Reason
	}{
		{"invalid report", invalid, ReasonInvalid},
		{"unknown robot", unknown, ReasonUnknownRobot},
		{"stamped beyond allowed clock skew", reportAt(serverNow.Add(6 * time.Second)), ReasonClockSkew},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newFixture()
			outcomes := ingest(t, fixture, tt.report)

			assertOutcome(t, outcomes[0], StatusRejected, tt.reason)
			if outcomes[0].Detail == "" {
				t.Error("rejection should explain itself")
			}
			if len(fixture.history.records) != 0 || len(fixture.state.current) != 0 {
				t.Error("rejected report must not be stored")
			}
		})
	}
}

func TestIngestAcceptsReportWithinClockSkew(t *testing.T) {
	fixture := newFixture()
	outcomes := ingest(t, fixture, reportAt(serverNow.Add(5*time.Second)))
	assertOutcome(t, outcomes[0], StatusApplied, "")
}

func TestIngestAppliesNewReport(t *testing.T) {
	fixture := newFixture()
	report := reportAt(serverNow.Add(-time.Second))

	outcomes := ingest(t, fixture, report)

	assertOutcome(t, outcomes[0], StatusApplied, "")
	got := fixture.state.current[knownRobot]
	if !got.ReportedAt.Equal(report.ReportedAt) || !got.ReceivedAt.Equal(serverNow) {
		t.Fatalf("state = %+v, want reportedAt %s receivedAt %s", got, report.ReportedAt, serverNow)
	}
}

func TestIngestLateReportGoesToHistoryOnly(t *testing.T) {
	fixture := newFixture()
	newer := serverNow.Add(-time.Second)
	ingest(t, fixture, reportAt(newer))

	outcomes := ingest(t, fixture, reportAt(newer.Add(-time.Second)))

	assertOutcome(t, outcomes[0], StatusLate, "")
	if !fixture.state.current[knownRobot].ReportedAt.Equal(newer) {
		t.Fatal("late report overwrote the current state")
	}
	if len(fixture.history.records) != 2 {
		t.Fatalf("history has %d records, want 2", len(fixture.history.records))
	}
}

func TestIngestDuplicateChangesNothing(t *testing.T) {
	fixture := newFixture()
	report := reportAt(serverNow.Add(-time.Second))
	ingest(t, fixture, report)

	outcomes := ingest(t, fixture, report)

	assertOutcome(t, outcomes[0], StatusDuplicate, "")
	if len(fixture.history.records) != 1 {
		t.Fatalf("history has %d records, want 1", len(fixture.history.records))
	}
}

func TestIngestBatchOutOfOrder(t *testing.T) {
	fixture := newFixture()
	older, newer := serverNow.Add(-2*time.Second), serverNow.Add(-time.Second)

	outcomes := ingest(t, fixture, reportAt(newer), reportAt(older), reportAt(newer))

	assertOutcome(t, outcomes[0], StatusApplied, "")
	assertOutcome(t, outcomes[1], StatusLate, "")
	assertOutcome(t, outcomes[2], StatusDuplicate, "")
	if !fixture.state.current[knownRobot].ReportedAt.Equal(newer) {
		t.Fatal("state should hold the newest report of the batch")
	}
}

func TestIngestMixedBatchKeepsOrder(t *testing.T) {
	fixture := newFixture()
	unknown := validReport()
	unknown.Robot.SerialNumber = "NOT-REGISTERED"

	outcomes := ingest(t, fixture, unknown, reportAt(serverNow.Add(-time.Second)))

	assertOutcome(t, outcomes[0], StatusRejected, ReasonUnknownRobot)
	assertOutcome(t, outcomes[1], StatusApplied, "")
}

func TestIngestRetryAfterStateFailureAppliesState(t *testing.T) {
	fixture := newFixture()
	report := reportAt(serverNow.Add(-time.Second))
	fixture.state.err = errors.New("connection reset")

	if _, err := fixture.ingestor.Ingest(context.Background(), []Report{report}); err == nil {
		t.Fatal("want error when the state store fails")
	}

	fixture.state.err = nil
	outcomes := ingest(t, fixture, report)

	// History already had it, but the state was never written: the retry must apply it.
	assertOutcome(t, outcomes[0], StatusApplied, "")
	if _, ok := fixture.state.current[knownRobot]; !ok {
		t.Fatal("retry did not write the state")
	}
}

func TestIngestHistoryFailureFailsBatch(t *testing.T) {
	fixture := newFixture()
	fixture.history.err = errors.New("disk full")

	if _, err := fixture.ingestor.Ingest(context.Background(), []Report{reportAt(serverNow)}); err == nil {
		t.Fatal("want error when history fails")
	}
	if len(fixture.state.current) != 0 {
		t.Fatal("state must not move ahead of history")
	}
}
