package telemetry

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func validReport() Report {
	return Report{
		Robot:      Identity{Manufacturer: "acme", SerialNumber: "CLN-0042"},
		ReportedAt: time.Date(2026, 10, 30, 9, 15, 2, 0, time.UTC),
		Position:   Position{MapID: "hq-floor-2", X: 12.4, Y: 3.1, Theta: 1.57, Localized: true},
		Power:      Power{StateOfCharge: 18.5},
		Mode:       ModeAutomatic,
		Faults:     []Fault{{Type: "bumperPressed", Level: FaultWarning}},
		Safety:     Safety{EmergencyStop: EmergencyStopNone},
	}
}

func TestReportValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Report)
		wantErr string // substring; empty means valid
	}{
		{name: "valid report", mutate: func(*Report) {}},
		{name: "not localized without map is fine", mutate: func(r *Report) {
			r.Position = Position{Localized: false}
		}},
		{name: "battery at both bounds is fine", mutate: func(r *Report) { r.Power.StateOfCharge = 100 }},
		{name: "missing serial number", mutate: func(r *Report) { r.Robot.SerialNumber = "" }, wantErr: "robot identity"},
		{name: "missing timestamp", mutate: func(r *Report) { r.ReportedAt = time.Time{} }, wantErr: "reportedAt"},
		{name: "localized without map", mutate: func(r *Report) { r.Position.MapID = "" }, wantErr: "needs a map"},
		{name: "theta out of range", mutate: func(r *Report) { r.Position.Theta = math.Pi + 0.1 }, wantErr: "theta"},
		{name: "negative battery", mutate: func(r *Report) { r.Power.StateOfCharge = -1 }, wantErr: "state of charge"},
		{name: "battery above 100", mutate: func(r *Report) { r.Power.StateOfCharge = 100.1 }, wantErr: "state of charge"},
		{name: "unknown mode", mutate: func(r *Report) { r.Mode = "dancing" }, wantErr: "unknown mode"},
		{name: "fault without type", mutate: func(r *Report) { r.Faults[0].Type = "" }, wantErr: "has no type"},
		{name: "fault with unknown level", mutate: func(r *Report) { r.Faults[0].Level = "nal" }, wantErr: "unknown level"},
		{name: "unknown emergency stop", mutate: func(r *Report) { r.Safety.EmergencyStop = "maybe" }, wantErr: "emergency stop"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validReport()
			tt.mutate(&r)
			err := r.Validate()

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidReport) {
				t.Fatalf("want ErrInvalidReport, got %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestReportValidateCollectsAllProblems(t *testing.T) {
	r := validReport()
	r.Robot = Identity{}
	r.Power.StateOfCharge = 150
	r.Mode = ""

	err := r.Validate()
	for _, want := range []string{"robot identity", "state of charge", "unknown mode"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("want %q in %v", want, err)
		}
	}
}

func TestFaultLevelSevere(t *testing.T) {
	for level, want := range map[FaultLevel]bool{
		FaultWarning: false, FaultUrgent: false, FaultCritical: true, FaultFatal: true,
	} {
		if got := level.Severe(); got != want {
			t.Errorf("%s.Severe() = %v, want %v", level, got, want)
		}
	}
}

func TestEmergencyStopActive(t *testing.T) {
	for stop, want := range map[EmergencyStop]bool{
		EmergencyStopNone: false, EmergencyStopManual: true, EmergencyStopRemote: true,
	} {
		if got := stop.Active(); got != want {
			t.Errorf("%s.Active() = %v, want %v", stop, got, want)
		}
	}
}
