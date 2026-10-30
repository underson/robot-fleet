package vda

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/underson/robot-fleet/backend/internal/domain/telemetry"
)

const sample = `{
  "manufacturer": "acme",
  "serialNumber": "CLN-0042",
  "timestamp": "2026-10-30T09:15:02.120Z",
  "position": { "mapId": "hq-floor-2", "x": 12.4, "y": 3.1, "theta": 1.57, "localized": true },
  "powerSupply": { "stateOfCharge": 18.5, "charging": false },
  "operatingMode": "SEMIAUTOMATIC",
  "errors": [{ "errorType": "bumperPressed", "errorLevel": "WARNING", "errorDescription": "Front bumper contact" }],
  "safetyState": { "activeEmergencyStop": "REMOTE", "fieldViolation": true },
  "vendorData": { "brushWear": 0.34 },
  "someFutureField": "ignored"
}`

func TestDecodeTranslatesToDomain(t *testing.T) {
	report, err := Decode([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}

	if report.Robot != (telemetry.Identity{Manufacturer: "acme", SerialNumber: "CLN-0042"}) {
		t.Errorf("robot = %+v", report.Robot)
	}
	if want := time.Date(2026, 10, 30, 9, 15, 2, 120_000_000, time.UTC); !report.ReportedAt.Equal(want) {
		t.Errorf("reportedAt = %s, want %s", report.ReportedAt, want)
	}
	if report.Position != (telemetry.Position{MapID: "hq-floor-2", X: 12.4, Y: 3.1, Theta: 1.57, Localized: true}) {
		t.Errorf("position = %+v", report.Position)
	}
	if report.Power != (telemetry.Power{StateOfCharge: 18.5}) {
		t.Errorf("power = %+v", report.Power)
	}
	if report.Mode != telemetry.ModeSemiAutomatic {
		t.Errorf("mode = %q", report.Mode)
	}
	if len(report.Faults) != 1 || report.Faults[0] != (telemetry.Fault{Type: "bumperPressed", Level: telemetry.FaultWarning, Description: "Front bumper contact"}) {
		t.Errorf("faults = %+v", report.Faults)
	}
	if report.Safety != (telemetry.Safety{EmergencyStop: telemetry.EmergencyStopRemote, FieldViolation: true}) {
		t.Errorf("safety = %+v", report.Safety)
	}
	if string(report.VendorData) != `{ "brushWear": 0.34 }` {
		t.Errorf("vendorData = %s", report.VendorData)
	}
	if err := report.Validate(); err != nil {
		t.Errorf("translated sample should be valid: %v", err)
	}
}

func TestDecodeWithoutPositionIsNotLocalized(t *testing.T) {
	msg := strings.Replace(sample, `"position": { "mapId": "hq-floor-2", "x": 12.4, "y": 3.1, "theta": 1.57, "localized": true },`, "", 1)
	report, err := Decode([]byte(msg))
	if err != nil {
		t.Fatal(err)
	}
	if report.Position.Localized {
		t.Fatal("missing position must not be localized")
	}
}

func TestDecodeRejects(t *testing.T) {
	tests := []struct {
		name    string
		msg     string
		wantErr string
	}{
		{"not JSON", `{`, "unexpected end"},
		{"bad timestamp", strings.Replace(sample, "2026-10-30T09:15:02.120Z", "yesterday", 1), "cannot parse"},
		{"missing powerSupply", strings.Replace(sample, `"powerSupply": { "stateOfCharge": 18.5, "charging": false },`, "", 1), "powerSupply is required"},
		{"missing safetyState", strings.Replace(sample, `"safetyState": { "activeEmergencyStop": "REMOTE", "fieldViolation": true },`, "", 1), "safetyState is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode([]byte(tt.msg))
			if !errors.Is(err, telemetry.ErrInvalidReport) || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want ErrInvalidReport containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestUnknownEnumValuesReachValidate(t *testing.T) {
	msg := strings.Replace(sample, `"SEMIAUTOMATIC"`, `"DANCING"`, 1)
	report, err := Decode([]byte(msg))
	if err != nil {
		t.Fatalf("translation should pass unknown values through: %v", err)
	}
	if err := report.Validate(); err == nil || !strings.Contains(err.Error(), `"DANCING"`) {
		t.Fatalf("Validate should reject the wire value, got %v", err)
	}
}
