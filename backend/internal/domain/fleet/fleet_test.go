package fleet

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/underson/robot-fleet/backend/internal/domain/telemetry"
)

type fakeReader struct {
	robots  []RobotView
	history []HistoryPoint
	from    time.Time // last History call
}

func (r *fakeReader) Sites(context.Context) ([]Site, error) { return nil, nil }

func (r *fakeReader) Robots(_ context.Context, siteID string) ([]RobotView, error) {
	var out []RobotView
	for _, v := range r.robots {
		if siteID == "" || v.Robot.Site.ID == siteID {
			out = append(out, v)
		}
	}
	return out, nil
}

func (r *fakeReader) Robot(_ context.Context, id telemetry.RobotID) (RobotView, error) {
	for _, v := range r.robots {
		if v.Robot.ID == id {
			return v, nil
		}
	}
	return RobotView{}, ErrRobotNotFound
}

func (r *fakeReader) History(_ context.Context, _ telemetry.RobotID, from, _ time.Time) ([]HistoryPoint, error) {
	r.from = from
	return r.history, nil
}

func robot(name, site string, state *State) RobotView {
	return RobotView{Robot: Robot{ID: telemetry.RobotID(name), Name: name, Site: Site{ID: site}}, State: state}
}

func withState(mutate func(*State)) *State {
	s := healthyState()
	mutate(s)
	return s
}

func newFleet() (*Service, *fakeReader) {
	r := &fakeReader{robots: []RobotView{
		robot("bravo", "hq", healthyState()),
		robot("alpha", "hq", healthyState()),
		robot("charlie", "hq", withState(func(s *State) { s.ReceivedAt = now.Add(-time.Hour) })),
		robot("delta", "hq", withState(func(s *State) { s.Safety.EmergencyStop = telemetry.EmergencyStopRemote })),
		robot("echo", "airport", withState(func(s *State) { s.Power.StateOfCharge = 15 })),
		robot("foxtrot", "airport", nil),
	}}
	return NewService(r, DefaultHealthPolicy(), func() time.Time { return now }, 10*time.Minute), r
}

func names(robots []RobotStatus) []string {
	var out []string
	for _, r := range robots {
		out = append(out, r.Robot.Name)
	}
	return out
}

func TestOverviewSortsMostUrgentFirst(t *testing.T) {
	svc, _ := newFleet()
	got, err := svc.Overview(context.Background(), OverviewQuery{})
	if err != nil {
		t.Fatal(err)
	}

	// emergency stop, then offline, then the rest by name
	want := []string{"delta", "charlie", "alpha", "bravo", "echo", "foxtrot"}
	if g := names(got.Robots); !slices.Equal(g, want) {
		t.Fatalf("order = %v, want %v", g, want)
	}
}

func TestOverviewSummary(t *testing.T) {
	svc, _ := newFleet()
	got, _ := svc.Overview(context.Background(), OverviewQuery{Status: StatusAttention})

	want := Summary{Total: 6, Online: 4, Offline: 1, NeverReported: 1, NeedsAttention: 2, LowBattery: 1}
	if got.Summary != want {
		t.Fatalf("summary = %+v, want %+v (whole fleet, not just the filter)", got.Summary, want)
	}
}

func TestOverviewFilters(t *testing.T) {
	tests := []struct {
		name  string
		query OverviewQuery
		want  []string
	}{
		{"attention", OverviewQuery{Status: StatusAttention}, []string{"delta", "charlie"}},
		{"offline", OverviewQuery{Status: StatusOffline}, []string{"charlie"}},
		{"site", OverviewQuery{SiteID: "airport"}, []string{"echo", "foxtrot"}},
		{"page", OverviewQuery{Offset: 1, Limit: 2}, []string{"charlie", "alpha"}},
		{"page past the end", OverviewQuery{Offset: 10, Limit: 2}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newFleet()
			got, _ := svc.Overview(context.Background(), tt.query)
			if g := names(got.Robots); !slices.Equal(g, tt.want) {
				t.Fatalf("robots = %v, want %v", g, tt.want)
			}
		})
	}
}

func TestOverviewTotalCountsAllMatches(t *testing.T) {
	svc, _ := newFleet()
	got, _ := svc.Overview(context.Background(), OverviewQuery{Limit: 2})
	if got.Total != 6 || len(got.Robots) != 2 {
		t.Fatalf("total = %d, page = %d; want 6 and 2", got.Total, len(got.Robots))
	}
}

func TestRobotDetail(t *testing.T) {
	svc, reader := newFleet()
	reader.history = []HistoryPoint{{ReportedAt: now.Add(-time.Minute)}}

	got, err := svc.Robot(context.Background(), "delta")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Health.NeedsAttention() || len(got.History) != 1 {
		t.Fatalf("got %+v", got)
	}
	if want := now.Add(-10 * time.Minute); !reader.from.Equal(want) {
		t.Fatalf("history from %s, want %s", reader.from, want)
	}
}

func TestRobotDetailNotFound(t *testing.T) {
	svc, _ := newFleet()
	if _, err := svc.Robot(context.Background(), "nope"); !errors.Is(err, ErrRobotNotFound) {
		t.Fatalf("err = %v, want ErrRobotNotFound", err)
	}
}
