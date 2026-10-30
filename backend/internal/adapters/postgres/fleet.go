package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/underson/robot-fleet/backend/internal/domain/fleet"
	"github.com/underson/robot-fleet/backend/internal/domain/telemetry"
)

func (s *Store) Sites(ctx context.Context) ([]fleet.Site, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name FROM sites ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list sites: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (fleet.Site, error) {
		var site fleet.Site
		err := row.Scan(&site.ID, &site.Name)
		return site, err
	})
}

// robotViewQuery joins the registry with the current state. LEFT JOIN: a
// robot that never reported still shows up, with a NULL state.
const robotViewQuery = `
	SELECT r.id, r.name, r.manufacturer, r.serial_number, r.model, s.id, s.name,
	       st.reported_at, st.received_at, st.map_id, st.x, st.y, st.theta, st.localized,
	       st.state_of_charge, st.charging, st.mode, st.faults, st.emergency_stop, st.field_violation
	FROM robots r
	JOIN sites s ON s.id = r.site_id
	LEFT JOIN robot_state st ON st.robot_id = r.id`

func (s *Store) Robots(ctx context.Context, siteID string) ([]fleet.RobotView, error) {
	rows, err := s.pool.Query(ctx, robotViewQuery+` WHERE $1 = '' OR r.site_id = $1`, siteID)
	if err != nil {
		return nil, fmt.Errorf("list robots: %w", err)
	}
	return pgx.CollectRows(rows, scanRobotView)
}

func (s *Store) Robot(ctx context.Context, id telemetry.RobotID) (fleet.RobotView, error) {
	rows, err := s.pool.Query(ctx, robotViewQuery+` WHERE r.id = $1`, string(id))
	if err != nil {
		return fleet.RobotView{}, fmt.Errorf("get robot: %w", err)
	}
	view, err := pgx.CollectExactlyOneRow(rows, scanRobotView)
	if errors.Is(err, pgx.ErrNoRows) {
		return fleet.RobotView{}, fleet.ErrRobotNotFound
	}
	return view, err
}

func (s *Store) History(ctx context.Context, id telemetry.RobotID, from, to time.Time) ([]fleet.HistoryPoint, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT reported_at, map_id, x, y, theta, localized, state_of_charge, charging, mode, faults
		FROM telemetry
		WHERE robot_id = $1 AND reported_at >= $2 AND reported_at < $3
		ORDER BY reported_at`, string(id), from, to)
	if err != nil {
		return nil, fmt.Errorf("robot history: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (fleet.HistoryPoint, error) {
		var (
			point  fleet.HistoryPoint
			mode   string
			faults []byte
		)
		err := row.Scan(&point.ReportedAt, &point.Position.MapID, &point.Position.X, &point.Position.Y, &point.Position.Theta,
			&point.Position.Localized, &point.Power.StateOfCharge, &point.Power.Charging, &mode, &faults)
		if err != nil {
			return point, err
		}
		point.Mode = telemetry.Mode(mode)
		point.Faults, err = decodeFaults(faults)
		return point, err
	})
}

func scanRobotView(row pgx.CollectableRow) (fleet.RobotView, error) {
	var (
		view   fleet.RobotView
		stored struct {
			reportedAt, receivedAt *time.Time
			mapID, mode, eStop     *string
			x, y, theta, charge    *float64
			localized, charging    *bool
			fieldViolation         *bool
			faults                 []byte
		}
	)
	err := row.Scan(&view.Robot.ID, &view.Robot.Name, &view.Robot.Manufacturer, &view.Robot.SerialNumber, &view.Robot.Model,
		&view.Robot.Site.ID, &view.Robot.Site.Name,
		&stored.reportedAt, &stored.receivedAt, &stored.mapID, &stored.x, &stored.y, &stored.theta, &stored.localized,
		&stored.charge, &stored.charging, &stored.mode, &stored.faults, &stored.eStop, &stored.fieldViolation)
	if err != nil || stored.reportedAt == nil {
		return view, err // no state row: the robot never reported
	}

	faults, err := decodeFaults(stored.faults)
	if err != nil {
		return view, err
	}
	view.State = &fleet.State{
		ReportedAt: *stored.reportedAt,
		ReceivedAt: *stored.receivedAt,
		Position:   telemetry.Position{MapID: *stored.mapID, X: *stored.x, Y: *stored.y, Theta: *stored.theta, Localized: *stored.localized},
		Power:      telemetry.Power{StateOfCharge: *stored.charge, Charging: *stored.charging},
		Mode:       telemetry.Mode(*stored.mode),
		Faults:     faults,
		Safety:     telemetry.Safety{EmergencyStop: telemetry.EmergencyStop(*stored.eStop), FieldViolation: *stored.fieldViolation},
	}
	return view, nil
}
