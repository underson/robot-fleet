package fleet

import (
	"slices"
	"testing"
	"time"

	"github.com/underson/robot-fleet/backend/internal/domain/telemetry"
)

var now = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

func healthyState() *State {
	return &State{
		ReportedAt: now.Add(-time.Second),
		ReceivedAt: now.Add(-time.Second),
		Position:   telemetry.Position{MapID: "hq-floor-2", Localized: true},
		Power:      telemetry.Power{StateOfCharge: 80},
		Mode:       telemetry.ModeAutomatic,
		Safety:     telemetry.Safety{EmergencyStop: telemetry.EmergencyStopNone},
	}
}

func TestAssess(t *testing.T) {
	policy := DefaultHealthPolicy()
	tests := []struct {
		name         string
		mutate       func(*State)
		connectivity Connectivity
		battery      BatteryLevel
		attention    []AttentionReason
	}{
		{"healthy", func(*State) {}, Online, BatteryOK, nil},
		{"low battery is not yet attention", func(s *State) { s.Power.StateOfCharge = 15 }, Online, BatteryLow, nil},
		{"critical battery, not charging", func(s *State) { s.Power.StateOfCharge = 8 }, Online, BatteryCritical,
			[]AttentionReason{ReasonBatteryCritical}},
		{"critical battery while charging is fine", func(s *State) {
			s.Power = telemetry.Power{StateOfCharge: 8, Charging: true}
		}, Online, BatteryCritical, nil},
		{"offline uses server clock, not robot clock", func(s *State) {
			s.ReportedAt = now // robot clock says fresh
			s.ReceivedAt = now.Add(-11 * time.Second)
		}, Offline, BatteryOK, []AttentionReason{ReasonOffline}},
		{"exactly at the offline threshold is still online", func(s *State) {
			s.ReceivedAt = now.Add(-10 * time.Second)
		}, Online, BatteryOK, nil},
		{"warning fault is not attention", func(s *State) {
			s.Faults = []telemetry.Fault{{Type: "bumperPressed", Level: telemetry.FaultWarning}}
		}, Online, BatteryOK, nil},
		{"severe fault", func(s *State) {
			s.Faults = []telemetry.Fault{{Type: "motorOverheat", Level: telemetry.FaultCritical}}
		}, Online, BatteryOK, []AttentionReason{ReasonSevereFault}},
		{"several reasons, most urgent first", func(s *State) {
			s.ReceivedAt = now.Add(-time.Minute)
			s.Power.StateOfCharge = 5
			s.Safety.EmergencyStop = telemetry.EmergencyStopManual
		}, Offline, BatteryCritical, []AttentionReason{ReasonEmergencyStop, ReasonOffline, ReasonBatteryCritical}},
		{"all reasons come back in priority order", func(s *State) {
			s.ReceivedAt = now.Add(-time.Minute)
			s.Power.StateOfCharge = 5
			s.Faults = []telemetry.Fault{{Type: "motorOverheat", Level: telemetry.FaultFatal}}
			s.Safety.EmergencyStop = telemetry.EmergencyStopManual
		}, Offline, BatteryCritical, reasonPriority},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := healthyState()
			tt.mutate(s)
			h := policy.Assess(s, now)

			if h.Connectivity != tt.connectivity || h.Battery != tt.battery || !slices.Equal(h.Attention, tt.attention) {
				t.Fatalf("got %+v, want connectivity %s battery %s attention %v", h, tt.connectivity, tt.battery, tt.attention)
			}
		})
	}
}

func TestAssessNeverReported(t *testing.T) {
	h := DefaultHealthPolicy().Assess(nil, now)
	if h.Connectivity != NeverReported || h.NeedsAttention() {
		t.Fatalf("got %+v, want never_reported without attention", h)
	}
}

func TestAssessUnlocalizedPosition(t *testing.T) {
	s := healthyState()
	s.Position.Localized = false
	if DefaultHealthPolicy().Assess(s, now).PositionKnown {
		t.Fatal("position should be unknown when the robot isn't localized")
	}
}
