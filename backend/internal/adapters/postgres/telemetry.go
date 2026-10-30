package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/underson/robot-fleet/backend/internal/domain/telemetry"
)

func (s *Store) Lookup(ctx context.Context, id telemetry.Identity) (telemetry.RobotID, error) {
	var robotID string
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM robots WHERE manufacturer = $1 AND serial_number = $2`,
		id.Manufacturer, id.SerialNumber,
	).Scan(&robotID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", telemetry.ErrUnknownRobot
	}
	if err != nil {
		return "", fmt.Errorf("look up robot: %w", err)
	}
	return telemetry.RobotID(robotID), nil
}

const recordColumns = `robot_id, reported_at, received_at, map_id, x, y, theta, localized,
	state_of_charge, charging, mode, faults, emergency_stop, field_violation, vendor_data`

const recordValues = `$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15`

// Append sends all inserts in one round trip. ON CONFLICT DO NOTHING on the
// (robot_id, reported_at) key turns a duplicate into "0 rows", not an error.
func (s *Store) Append(ctx context.Context, records []telemetry.Record) ([]bool, error) {
	batch := &pgx.Batch{}
	for _, rec := range records {
		args, err := recordArgs(rec)
		if err != nil {
			return nil, err
		}
		batch.Queue(`INSERT INTO telemetry (`+recordColumns+`) VALUES (`+recordValues+`)
			ON CONFLICT (robot_id, reported_at) DO NOTHING`, args...)
	}

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()
	inserted := make([]bool, len(records))
	for i := range records {
		tag, err := results.Exec()
		if err != nil {
			return nil, fmt.Errorf("insert telemetry: %w", err)
		}
		inserted[i] = tag.RowsAffected() == 1
	}
	return inserted, results.Close()
}

// SaveIfNewer is the ordering rule (telemetry.Record.Supersedes) as one atomic
// statement: the WHERE on the conflict branch skips the update unless the
// incoming report is strictly newer. Doing this as read-compare-write in Go
// would race between two concurrent reports for the same robot.
func (s *Store) SaveIfNewer(ctx context.Context, rec telemetry.Record) (bool, error) {
	args, err := recordArgs(rec)
	if err != nil {
		return false, err
	}
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO robot_state (`+recordColumns+`) VALUES (`+recordValues+`)
		ON CONFLICT (robot_id) DO UPDATE SET
			reported_at = excluded.reported_at, received_at = excluded.received_at,
			map_id = excluded.map_id, x = excluded.x, y = excluded.y, theta = excluded.theta,
			localized = excluded.localized, state_of_charge = excluded.state_of_charge,
			charging = excluded.charging, mode = excluded.mode, faults = excluded.faults,
			emergency_stop = excluded.emergency_stop, field_violation = excluded.field_violation,
			vendor_data = excluded.vendor_data
		WHERE robot_state.reported_at < excluded.reported_at`, args...)
	if err != nil {
		return false, fmt.Errorf("save state: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// fault is the stored JSON shape of telemetry.Fault, kept here so a renamed
// domain field can't silently change stored data.
type fault struct {
	Type        string `json:"type"`
	Level       string `json:"level"`
	Description string `json:"description,omitempty"`
}

func recordArgs(rec telemetry.Record) ([]any, error) {
	faults := make([]fault, len(rec.Faults))
	for i, f := range rec.Faults {
		faults[i] = fault{Type: f.Type, Level: string(f.Level), Description: f.Description}
	}
	faultsJSON, err := json.Marshal(faults)
	if err != nil {
		return nil, fmt.Errorf("encode faults: %w", err)
	}
	var vendorData []byte
	if len(rec.VendorData) > 0 {
		vendorData = rec.VendorData
	}
	p := rec.Position
	return []any{
		string(rec.RobotID), rec.ReportedAt, rec.ReceivedAt, p.MapID, p.X, p.Y, p.Theta, p.Localized,
		rec.Power.StateOfCharge, rec.Power.Charging, string(rec.Mode), faultsJSON,
		string(rec.Safety.EmergencyStop), rec.Safety.FieldViolation, vendorData,
	}, nil
}

func decodeFaults(data []byte) ([]telemetry.Fault, error) {
	var stored []fault
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("decode faults: %w", err)
	}
	var out []telemetry.Fault
	for _, f := range stored {
		out = append(out, telemetry.Fault{Type: f.Type, Level: telemetry.FaultLevel(f.Level), Description: f.Description})
	}
	return out, nil
}
