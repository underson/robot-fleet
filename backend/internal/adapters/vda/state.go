// Package vda translates the VDA 5050-inspired state message into telemetry
// domain types. It is the anti-corruption layer: wire names, wire enum values
// and wire quirks stop here. Supporting another vendor format means adding a
// sibling package, not changing the domain.
package vda

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/underson/robot-fleet/backend/internal/domain/telemetry"
)

// StateMessage is the wire format. Field names and enum values follow
// VDA 5050 3.0 where the concept exists there.
type StateMessage struct {
	Manufacturer  string          `json:"manufacturer"`
	SerialNumber  string          `json:"serialNumber"`
	Timestamp     time.Time       `json:"timestamp"`
	Position      *Position       `json:"position,omitempty"` // optional, as in VDA 5050
	PowerSupply   *PowerSupply    `json:"powerSupply"`
	OperatingMode string          `json:"operatingMode"`
	Errors        []Error         `json:"errors"`
	SafetyState   *SafetyState    `json:"safetyState"`
	VendorData    json.RawMessage `json:"vendorData,omitempty"`
}

type Position struct {
	MapID     string  `json:"mapId"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Theta     float64 `json:"theta"`
	Localized bool    `json:"localized"`
}

type PowerSupply struct {
	StateOfCharge float64 `json:"stateOfCharge"`
	Charging      bool    `json:"charging"`
}

type Error struct {
	ErrorType        string `json:"errorType"`
	ErrorLevel       string `json:"errorLevel"`
	ErrorDescription string `json:"errorDescription,omitempty"`
}

type SafetyState struct {
	ActiveEmergencyStop string `json:"activeEmergencyStop"`
	FieldViolation      bool   `json:"fieldViolation"`
}

var modes = map[string]telemetry.Mode{
	"STARTUP":       telemetry.ModeStartup,
	"AUTOMATIC":     telemetry.ModeAutomatic,
	"SEMIAUTOMATIC": telemetry.ModeSemiAutomatic,
	"INTERVENED":    telemetry.ModeIntervened,
	"MANUAL":        telemetry.ModeManual,
	"SERVICE":       telemetry.ModeService,
	"TEACH_IN":      telemetry.ModeTeachIn,
}

var faultLevels = map[string]telemetry.FaultLevel{
	"WARNING":  telemetry.FaultWarning,
	"URGENT":   telemetry.FaultUrgent,
	"CRITICAL": telemetry.FaultCritical,
	"FATAL":    telemetry.FaultFatal,
}

var emergencyStops = map[string]telemetry.EmergencyStop{
	"NONE":   telemetry.EmergencyStopNone,
	"MANUAL": telemetry.EmergencyStopManual,
	"REMOTE": telemetry.EmergencyStopRemote,
}

// Decode parses one message and translates it. Errors wrap
// telemetry.ErrInvalidReport, so callers reject the message like any other
// invalid report. Unknown fields are ignored: newer robots may send more.
func Decode(data []byte) (telemetry.Report, error) {
	var m StateMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return telemetry.Report{}, fmt.Errorf("%w: %w", telemetry.ErrInvalidReport, err)
	}
	return m.ToReport()
}

// ToReport translates the message into domain terms. It only checks what the
// wire format alone can tell (required objects present); business rules are
// left to telemetry.Report.Validate. Unknown enum values are passed through
// unchanged so Validate rejects them with the value in the message.
func (m StateMessage) ToReport() (telemetry.Report, error) {
	// A missing powerSupply must not read as 0 % battery, and a missing
	// safetyState must not read as "no emergency stop".
	var missing []error
	if m.PowerSupply == nil {
		missing = append(missing, errors.New("powerSupply is required"))
	}
	if m.SafetyState == nil {
		missing = append(missing, errors.New("safetyState is required"))
	}
	if len(missing) > 0 {
		return telemetry.Report{}, fmt.Errorf("%w: %w", telemetry.ErrInvalidReport, errors.Join(missing...))
	}

	report := telemetry.Report{
		Robot:      telemetry.Identity{Manufacturer: m.Manufacturer, SerialNumber: m.SerialNumber},
		ReportedAt: m.Timestamp,
		Power:      telemetry.Power{StateOfCharge: m.PowerSupply.StateOfCharge, Charging: m.PowerSupply.Charging},
		Mode:       lookup(modes, m.OperatingMode),
		Safety: telemetry.Safety{
			EmergencyStop:  lookup(emergencyStops, m.SafetyState.ActiveEmergencyStop),
			FieldViolation: m.SafetyState.FieldViolation,
		},
		VendorData: m.VendorData,
	}
	if m.Position != nil {
		report.Position = telemetry.Position{
			MapID: m.Position.MapID, X: m.Position.X, Y: m.Position.Y,
			Theta: m.Position.Theta, Localized: m.Position.Localized,
		}
	}
	for _, wireError := range m.Errors {
		report.Faults = append(report.Faults, telemetry.Fault{
			Type:        wireError.ErrorType,
			Level:       lookup(faultLevels, wireError.ErrorLevel),
			Description: wireError.ErrorDescription,
		})
	}
	return report, nil
}

func lookup[T ~string](values map[string]T, wire string) T {
	if domainValue, ok := values[wire]; ok {
		return domainValue
	}
	return T(wire)
}
