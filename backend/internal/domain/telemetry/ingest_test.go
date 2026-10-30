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
	for i, rec := range records {
		k := historyKey{rec.RobotID, rec.ReportedAt}
		if _, ok := h.records[k]; !ok {
			h.records[k] = rec
			inserted[i] = true
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
	if cur, ok := s.current[rec.RobotID]; ok && !rec.Supersedes(cur) {
		return false, nil
	}
	s.current[rec.RobotID] = rec
	return true, nil
}

type fixture struct {
	ingestor *Ingestor
	history  *fakeHistory
	state    *fakeState
}

func newFixture() fixture {
	h := &fakeHistory{records: map[historyKey]Record{}}
	s := &fakeState{current: map[RobotID]Record{}}
	return fixture{
		ingestor: NewIngestor(fakeRegistry{}, h, s, func() time.Time { return serverNow }, 5*time.Second),
		history:  h,
		state:    s,
	}
}

func reportAt(at time.Time) Report {
	r := validReport()
	r.ReportedAt = at
	return r
}

func ingest(t *testing.T, f fixture, reports ...Report) []Outcome {
	t.Helper()
	out, err := f.ingestor.Ingest(context.Background(), reports)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return out
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
			f := newFixture()
			out := ingest(t, f, tt.report)

			assertOutcome(t, out[0], StatusRejected, tt.reason)
			if out[0].Detail == "" {
				t.Error("rejection should explain itself")
			}
			if len(f.history.records) != 0 || len(f.state.current) != 0 {
				t.Error("rejected report must not be stored")
			}
		})
	}
}

func TestIngestAcceptsReportWithinClockSkew(t *testing.T) {
	f := newFixture()
	out := ingest(t, f, reportAt(serverNow.Add(5*time.Second)))
	assertOutcome(t, out[0], StatusApplied, "")
}

func TestIngestAppliesNewReport(t *testing.T) {
	f := newFixture()
	rep := reportAt(serverNow.Add(-time.Second))

	out := ingest(t, f, rep)

	assertOutcome(t, out[0], StatusApplied, "")
	got := f.state.current[knownRobot]
	if !got.ReportedAt.Equal(rep.ReportedAt) || !got.ReceivedAt.Equal(serverNow) {
		t.Fatalf("state = %+v, want reportedAt %s receivedAt %s", got, rep.ReportedAt, serverNow)
	}
}

func TestIngestLateReportGoesToHistoryOnly(t *testing.T) {
	f := newFixture()
	newer := serverNow.Add(-time.Second)
	ingest(t, f, reportAt(newer))

	out := ingest(t, f, reportAt(newer.Add(-time.Second)))

	assertOutcome(t, out[0], StatusLate, "")
	if !f.state.current[knownRobot].ReportedAt.Equal(newer) {
		t.Fatal("late report overwrote the current state")
	}
	if len(f.history.records) != 2 {
		t.Fatalf("history has %d records, want 2", len(f.history.records))
	}
}

func TestIngestDuplicateChangesNothing(t *testing.T) {
	f := newFixture()
	rep := reportAt(serverNow.Add(-time.Second))
	ingest(t, f, rep)

	out := ingest(t, f, rep)

	assertOutcome(t, out[0], StatusDuplicate, "")
	if len(f.history.records) != 1 {
		t.Fatalf("history has %d records, want 1", len(f.history.records))
	}
}

func TestIngestBatchOutOfOrder(t *testing.T) {
	f := newFixture()
	older, newer := serverNow.Add(-2*time.Second), serverNow.Add(-time.Second)

	out := ingest(t, f, reportAt(newer), reportAt(older), reportAt(newer))

	assertOutcome(t, out[0], StatusApplied, "")
	assertOutcome(t, out[1], StatusLate, "")
	assertOutcome(t, out[2], StatusDuplicate, "")
	if !f.state.current[knownRobot].ReportedAt.Equal(newer) {
		t.Fatal("state should hold the newest report of the batch")
	}
}

func TestIngestMixedBatchKeepsOrder(t *testing.T) {
	f := newFixture()
	unknown := validReport()
	unknown.Robot.SerialNumber = "NOT-REGISTERED"

	out := ingest(t, f, unknown, reportAt(serverNow.Add(-time.Second)))

	assertOutcome(t, out[0], StatusRejected, ReasonUnknownRobot)
	assertOutcome(t, out[1], StatusApplied, "")
}

func TestIngestRetryAfterStateFailureAppliesState(t *testing.T) {
	f := newFixture()
	rep := reportAt(serverNow.Add(-time.Second))
	f.state.err = errors.New("connection reset")

	if _, err := f.ingestor.Ingest(context.Background(), []Report{rep}); err == nil {
		t.Fatal("want error when the state store fails")
	}

	f.state.err = nil
	out := ingest(t, f, rep)

	// History already had it, but the state was never written: the retry must apply it.
	assertOutcome(t, out[0], StatusApplied, "")
	if _, ok := f.state.current[knownRobot]; !ok {
		t.Fatal("retry did not write the state")
	}
}

func TestIngestHistoryFailureFailsBatch(t *testing.T) {
	f := newFixture()
	f.history.err = errors.New("disk full")

	if _, err := f.ingestor.Ingest(context.Background(), []Report{reportAt(serverNow)}); err == nil {
		t.Fatal("want error when history fails")
	}
	if len(f.state.current) != 0 {
		t.Fatal("state must not move ahead of history")
	}
}
