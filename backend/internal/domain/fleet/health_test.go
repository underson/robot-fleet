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
		{"low battery is not yet attention", func(state *State) { state.Power.StateOfCharge = 15 }, Online, BatteryLow, nil},
		{"critical battery, not charging", func(state *State) { state.Power.StateOfCharge = 8 }, Online, BatteryCritical,
			[]AttentionReason{ReasonBatteryCritical}},
		{"critical battery while charging is fine", func(state *State) {
			state.Power = telemetry.Power{StateOfCharge: 8, Charging: true}
		}, Online, BatteryCritical, nil},
		{"offline uses server clock, not robot clock", func(state *State) {
			state.ReportedAt = now // robot clock says fresh
			state.ReceivedAt = now.Add(-11 * time.Second)
		}, Offline, BatteryOK, []AttentionReason{ReasonOffline}},
		{"exactly at the offline threshold is still online", func(state *State) {
			state.ReceivedAt = now.Add(-10 * time.Second)
		}, Online, BatteryOK, nil},
		{"warning fault is not attention", func(state *State) {
			state.Faults = []telemetry.Fault{{Type: "bumperPressed", Level: telemetry.FaultWarning}}
		}, Online, BatteryOK, nil},
		{"severe fault", func(state *State) {
			state.Faults = []telemetry.Fault{{Type: "motorOverheat", Level: telemetry.FaultCritical}}
		}, Online, BatteryOK, []AttentionReason{ReasonSevereFault}},
		{"several reasons, most urgent first", func(state *State) {
			state.ReceivedAt = now.Add(-time.Minute)
			state.Power.StateOfCharge = 5
			state.Safety.EmergencyStop = telemetry.EmergencyStopManual
		}, Offline, BatteryCritical, []AttentionReason{ReasonEmergencyStop, ReasonOffline, ReasonBatteryCritical}},
		{"all reasons come back in priority order", func(state *State) {
			state.ReceivedAt = now.Add(-time.Minute)
			state.Power.StateOfCharge = 5
			state.Faults = []telemetry.Fault{{Type: "motorOverheat", Level: telemetry.FaultFatal}}
			state.Safety.EmergencyStop = telemetry.EmergencyStopManual
		}, Offline, BatteryCritical, reasonPriority},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := healthyState()
			tt.mutate(state)
			health := policy.Assess(state, now)

			if health.Connectivity != tt.connectivity || health.Battery != tt.battery || !slices.Equal(health.Attention, tt.attention) {
				t.Fatalf("got %+v, want connectivity %state battery %state attention %v", health, tt.connectivity, tt.battery, tt.attention)
			}
		})
	}
}

func TestAssessNeverReported(t *testing.T) {
	health := DefaultHealthPolicy().Assess(nil, now)
	if health.Connectivity != NeverReported || health.NeedsAttention() {
		t.Fatalf("got %+v, want never_reported without attention", health)
	}
}

func TestAssessUnlocalizedPosition(t *testing.T) {
	state := healthyState()
	state.Position.Localized = false
	if DefaultHealthPolicy().Assess(state, now).PositionKnown {
		t.Fatal("position should be unknown when the robot isn't localized")
	}
}
