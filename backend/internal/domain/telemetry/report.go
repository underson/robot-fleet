// Package telemetry is the write side: what a robot reports, the rules for
// accepting a report, and the ports the ingestion use case needs.
package telemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

// ErrInvalidReport marks a report that breaks the domain's rules. Callers use
// errors.Is to tell it apart from infrastructure failures.
var ErrInvalidReport = errors.New("invalid report")

// Identity is how a robot identifies itself: unique across vendors, but not
// our ID. The registry maps it to a RobotID.
type Identity struct {
	Manufacturer string
	SerialNumber string
}

// Report is one state message from a robot, already translated from its wire
// format into domain terms.
type Report struct {
	Robot      Identity
	ReportedAt time.Time // robot clock: decides ordering, never liveness
	Position   Position
	Power      Power
	Mode       Mode
	Faults     []Fault
	Safety     Safety
	VendorData json.RawMessage // manufacturer-specific, stored but not interpreted
}

// Position is on a building map, not GPS. When Localized is false the robot
// doesn't trust its own coordinates.
type Position struct {
	MapID     string
	X, Y      float64 // metres
	Theta     float64 // radians, [-π, π]
	Localized bool
}

type Power struct {
	StateOfCharge float64 // percent, [0, 100]
	Charging      bool
}

type Mode string

const (
	ModeStartup       Mode = "startup"
	ModeAutomatic     Mode = "automatic"
	ModeSemiAutomatic Mode = "semi_automatic"
	ModeIntervened    Mode = "intervened"
	ModeManual        Mode = "manual"
	ModeService       Mode = "service"
	ModeTeachIn       Mode = "teach_in"
)

func (m Mode) valid() bool {
	switch m {
	case ModeStartup, ModeAutomatic, ModeSemiAutomatic, ModeIntervened, ModeManual, ModeService, ModeTeachIn:
		return true
	}
	return false
}

// Fault is a problem the robot reports about itself. Named Fault, not Error,
// so it never reads like a Go error.
type Fault struct {
	Type        string
	Level       FaultLevel
	Description string
}

type FaultLevel string

const (
	FaultWarning  FaultLevel = "warning"
	FaultUrgent   FaultLevel = "urgent"
	FaultCritical FaultLevel = "critical"
	FaultFatal    FaultLevel = "fatal"
)

// Severe reports whether the fault needs a human: the robot can't safely
// continue on its own.
func (l FaultLevel) Severe() bool {
	return l == FaultCritical || l == FaultFatal
}

func (l FaultLevel) valid() bool {
	switch l {
	case FaultWarning, FaultUrgent, FaultCritical, FaultFatal:
		return true
	}
	return false
}

type Safety struct {
	EmergencyStop  EmergencyStop
	FieldViolation bool // something entered the robot's protective field
}

type EmergencyStop string

const (
	EmergencyStopNone   EmergencyStop = "none"
	EmergencyStopManual EmergencyStop = "manual" // pressed on the robot
	EmergencyStopRemote EmergencyStop = "remote" // triggered by an operator or system
)

func (e EmergencyStop) Active() bool {
	return e == EmergencyStopManual || e == EmergencyStopRemote
}

func (e EmergencyStop) valid() bool {
	return e == EmergencyStopNone || e.Active()
}

// Validate checks the report against the domain's rules and returns every
// problem at once, wrapped in ErrInvalidReport.
func (r Report) Validate() error {
	var problems []error
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Errorf(format, args...))
	}

	if r.Robot.Manufacturer == "" || r.Robot.SerialNumber == "" {
		add("robot identity needs manufacturer and serial number")
	}
	if r.ReportedAt.IsZero() {
		add("reportedAt is missing")
	}
	if r.Position.Localized && r.Position.MapID == "" {
		add("a localized position needs a map")
	}
	if r.Position.Theta < -math.Pi || r.Position.Theta > math.Pi {
		add("theta %v is outside [-π, π]", r.Position.Theta)
	}
	if r.Power.StateOfCharge < 0 || r.Power.StateOfCharge > 100 {
		add("state of charge %v%% is outside [0, 100]", r.Power.StateOfCharge)
	}
	if !r.Mode.valid() {
		add("unknown mode %q", r.Mode)
	}
	for i, f := range r.Faults {
		if f.Type == "" {
			add("fault %d has no type", i)
		}
		if !f.Level.valid() {
			add("fault %d has unknown level %q", i, f.Level)
		}
	}
	if !r.Safety.EmergencyStop.valid() {
		add("unknown emergency stop %q", r.Safety.EmergencyStop)
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrInvalidReport, errors.Join(problems...))
}
