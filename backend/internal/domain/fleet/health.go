// Package fleet is the read side: what a fleet manager sees. Health is
// derived here on every read, never stored.
package fleet

import (
	"cmp"
	"slices"
	"time"

	"github.com/underson/robot-fleet/backend/internal/domain/telemetry"
)

// HealthPolicy holds the thresholds that turn raw state into health. They are
// configuration, not constants: robots that report every 30 s (allowed by
// VDA 5050) need a longer OfflineAfter than ones reporting every second.
type HealthPolicy struct {
	OfflineAfter    time.Duration
	LowBattery      float64 // percent
	CriticalBattery float64 // percent
}

func DefaultHealthPolicy() HealthPolicy {
	return HealthPolicy{OfflineAfter: 10 * time.Second, LowBattery: 20, CriticalBattery: 10}
}

type Connectivity string

const (
	Online        Connectivity = "online"
	Offline       Connectivity = "offline"
	NeverReported Connectivity = "never_reported" // registered, no telemetry yet
)

type BatteryLevel string

const (
	BatteryOK       BatteryLevel = "ok"
	BatteryLow      BatteryLevel = "low"
	BatteryCritical BatteryLevel = "critical"
	BatteryUnknown  BatteryLevel = "unknown"
)

// AttentionReason says why a robot needs a human. The order of the constants
// is the priority: the first reason is the most urgent.
type AttentionReason string

const (
	ReasonEmergencyStop   AttentionReason = "emergency_stop"
	ReasonSevereFault     AttentionReason = "severe_fault"
	ReasonOffline         AttentionReason = "offline"
	ReasonBatteryCritical AttentionReason = "battery_critical"
)

var reasonPriority = []AttentionReason{ReasonEmergencyStop, ReasonSevereFault, ReasonOffline, ReasonBatteryCritical}

type Health struct {
	Connectivity  Connectivity
	Battery       BatteryLevel
	PositionKnown bool
	Attention     []AttentionReason // most urgent first; empty when all is well
}

func (h Health) NeedsAttention() bool { return len(h.Attention) > 0 }

// priority is the reason's rank in reasonPriority: lower is more urgent.
func (r AttentionReason) priority() int {
	return slices.Index(reasonPriority, r)
}

// urgency ranks a robot for sorting by its most urgent reason: lower is more
// urgent, and robots without reasons come after all others.
func (h Health) urgency() int {
	if !h.NeedsAttention() {
		return len(reasonPriority)
	}
	return h.Attention[0].priority()
}

// Assess derives health from the robot's last state, as seen at now.
// A nil state means the robot never reported.
func (p HealthPolicy) Assess(state *State, now time.Time) Health {
	if state == nil {
		return Health{Connectivity: NeverReported, Battery: BatteryUnknown}
	}

	health := Health{
		Connectivity:  Online,
		Battery:       p.batteryLevel(state.Power.StateOfCharge),
		PositionKnown: state.Position.Localized,
	}
	// Liveness uses the server clock: robot clocks drift.
	if now.Sub(state.ReceivedAt) > p.OfflineAfter {
		health.Connectivity = Offline
	}

	var reasons []AttentionReason
	if state.Safety.EmergencyStop.Active() {
		reasons = append(reasons, ReasonEmergencyStop)
	}
	if slices.ContainsFunc(state.Faults, func(fault telemetry.Fault) bool { return fault.Level.Severe() }) {
		reasons = append(reasons, ReasonSevereFault)
	}
	if health.Connectivity == Offline {
		reasons = append(reasons, ReasonOffline)
	}
	// A critical robot on its charger is doing the right thing.
	if health.Battery == BatteryCritical && !state.Power.Charging {
		reasons = append(reasons, ReasonBatteryCritical)
	}
	// Order by priority here, so the order is defined only by reasonPriority,
	// not by the order of the checks above.
	slices.SortFunc(reasons, func(a, b AttentionReason) int {
		return cmp.Compare(a.priority(), b.priority())
	})
	health.Attention = reasons
	return health
}

func (p HealthPolicy) batteryLevel(stateOfCharge float64) BatteryLevel {
	switch {
	case stateOfCharge <= p.CriticalBattery:
		return BatteryCritical
	case stateOfCharge <= p.LowBattery:
		return BatteryLow
	default:
		return BatteryOK
	}
}
