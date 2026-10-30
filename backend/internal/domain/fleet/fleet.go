package fleet

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/underson/robot-fleet/backend/internal/domain/telemetry"
)

var ErrRobotNotFound = errors.New("robot not found")

type Site struct {
	ID   string
	Name string
}

type Robot struct {
	ID           telemetry.RobotID
	Name         string
	Site         Site
	Manufacturer string
	SerialNumber string
	Model        string
}

// State is a robot's last accepted report as the read side sees it. The read
// side reuses telemetry's value types (shared vocabulary) but never its use
// cases; telemetry doesn't know fleet exists.
type State struct {
	ReportedAt time.Time
	ReceivedAt time.Time
	Position   telemetry.Position
	Power      telemetry.Power
	Mode       telemetry.Mode
	Faults     []telemetry.Fault
	Safety     telemetry.Safety
}

// RobotView is a robot with its last state (nil if it never reported).
type RobotView struct {
	Robot Robot
	State *State
}

// HistoryPoint is one stored report, trimmed to what the detail view shows.
type HistoryPoint struct {
	ReportedAt time.Time
	Position   telemetry.Position
	Power      telemetry.Power
	Mode       telemetry.Mode
	Faults     []telemetry.Fault
}

// Reader is the read side's port to storage.
type Reader interface {
	Sites(ctx context.Context) ([]Site, error)
	// Robots returns all robots, or only those at siteID when it isn't empty.
	Robots(ctx context.Context, siteID string) ([]RobotView, error)
	Robot(ctx context.Context, id telemetry.RobotID) (RobotView, error) // ErrRobotNotFound if missing
	// History returns reports with from <= ReportedAt < to, oldest first.
	History(ctx context.Context, id telemetry.RobotID, from, to time.Time) ([]HistoryPoint, error)
}

type StatusFilter string

const (
	StatusAll       StatusFilter = ""
	StatusAttention StatusFilter = "attention"
	StatusOnline    StatusFilter = "online"
	StatusOffline   StatusFilter = "offline"
)

type OverviewQuery struct {
	SiteID string
	Status StatusFilter
	Offset int
	Limit  int
}

type Summary struct {
	Total          int
	Online         int
	Offline        int
	NeverReported  int
	NeedsAttention int
	LowBattery     int // low or critical
}

type RobotStatus struct {
	RobotView
	Health Health
}

type Overview struct {
	Summary Summary       // for the whole site (or fleet), before the status filter
	Robots  []RobotStatus // one page, most urgent first
	Total   int           // robots matching the filter, for paging
}

type RobotDetail struct {
	RobotStatus
	History []HistoryPoint
}

type Service struct {
	reader  Reader
	policy  HealthPolicy
	now     func() time.Time
	history time.Duration // how far back the detail view looks
}

func NewService(reader Reader, policy HealthPolicy, now func() time.Time, history time.Duration) *Service {
	return &Service{reader: reader, policy: policy, now: now, history: history}
}

func (s *Service) Sites(ctx context.Context) ([]Site, error) {
	return s.reader.Sites(ctx)
}

// Overview derives health for every robot in scope, then filters, sorts and
// pages in memory. Fine for thousands of robots; at 100x health becomes a
// stored, event-maintained column so the database can filter and page
// (docs/DESIGN.md §7).
func (s *Service) Overview(ctx context.Context, q OverviewQuery) (Overview, error) {
	views, err := s.reader.Robots(ctx, q.SiteID)
	if err != nil {
		return Overview{}, fmt.Errorf("list robots: %w", err)
	}

	now := s.now()
	var summary Summary
	var matching []RobotStatus
	for _, v := range views {
		h := s.policy.Assess(v.State, now)
		summary.add(h)
		if q.Status.matches(h) {
			matching = append(matching, RobotStatus{RobotView: v, Health: h})
		}
	}

	slices.SortStableFunc(matching, func(a, b RobotStatus) int {
		return cmp.Or(
			cmp.Compare(a.Health.urgency(), b.Health.urgency()),
			cmp.Compare(a.Robot.Name, b.Robot.Name),
		)
	})

	return Overview{Summary: summary, Robots: page(matching, q.Offset, q.Limit), Total: len(matching)}, nil
}

func (s *Service) Robot(ctx context.Context, id telemetry.RobotID) (RobotDetail, error) {
	v, err := s.reader.Robot(ctx, id)
	if err != nil {
		return RobotDetail{}, err
	}
	now := s.now()
	history, err := s.reader.History(ctx, id, now.Add(-s.history), now)
	if err != nil {
		return RobotDetail{}, fmt.Errorf("robot history: %w", err)
	}
	return RobotDetail{
		RobotStatus: RobotStatus{RobotView: v, Health: s.policy.Assess(v.State, now)},
		History:     history,
	}, nil
}

func (sum *Summary) add(h Health) {
	sum.Total++
	switch h.Connectivity {
	case Online:
		sum.Online++
	case Offline:
		sum.Offline++
	case NeverReported:
		sum.NeverReported++
	}
	if h.NeedsAttention() {
		sum.NeedsAttention++
	}
	if h.Battery == BatteryLow || h.Battery == BatteryCritical {
		sum.LowBattery++
	}
}

func (f StatusFilter) matches(h Health) bool {
	switch f {
	case StatusAttention:
		return h.NeedsAttention()
	case StatusOnline:
		return h.Connectivity == Online
	case StatusOffline:
		return h.Connectivity == Offline
	default:
		return true
	}
}

func page[T any](items []T, offset, limit int) []T {
	if offset >= len(items) {
		return nil
	}
	end := len(items)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return items[offset:end]
}
