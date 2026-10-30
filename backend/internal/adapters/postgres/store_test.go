package postgres

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/underson/robot-fleet/backend/internal/domain/fleet"
	"github.com/underson/robot-fleet/backend/internal/domain/telemetry"
)

// newStore starts a real Postgres in Docker, migrates it and seeds one site
// with two robots. Skipped with -short.
func newStore(t *testing.T) *Store {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test: needs Docker")
	}
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("fleet"), tcpostgres.WithUsername("fleet"), tcpostgres.WithPassword("fleet"),
		tcpostgres.BasicWaitStrategies())
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO sites (id, name) VALUES ('hq', 'Headquarters');
		INSERT INTO robots (id, site_id, name, manufacturer, serial_number, model) VALUES
			('robot-1', 'hq', 'Cleaner 1', 'acme', 'CLN-0042', 'C-200'),
			('robot-2', 'hq', 'Cleaner 2', 'acme', 'CLN-0043', 'C-200')`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return NewStore(pool)
}

var base = time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)

func record(robot telemetry.RobotID, reportedAt time.Time, charge float64) telemetry.Record {
	return telemetry.Record{
		RobotID:    robot,
		ReceivedAt: reportedAt.Add(100 * time.Millisecond),
		Report: telemetry.Report{
			ReportedAt: reportedAt,
			Position:   telemetry.Position{MapID: "hq-floor-2", X: 1, Y: 2, Theta: 0.5, Localized: true},
			Power:      telemetry.Power{StateOfCharge: charge},
			Mode:       telemetry.ModeAutomatic,
			Faults:     []telemetry.Fault{{Type: "motorOverheat", Level: telemetry.FaultCritical}},
			Safety:     telemetry.Safety{EmergencyStop: telemetry.EmergencyStopNone},
		},
	}
}

func TestSaveIfNewerAppliesOnlyNewerReports(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	steps := []struct {
		name      string
		at        time.Duration
		charge    float64
		wantSaved bool
	}{
		{"first report", 2 * time.Second, 50, true},
		{"older report", 1 * time.Second, 40, false},
		{"same timestamp", 2 * time.Second, 30, false},
		{"newer report", 3 * time.Second, 20, true},
	}
	for _, step := range steps {
		saved, err := s.SaveIfNewer(ctx, record("robot-1", base.Add(step.at), step.charge))
		if err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if saved != step.wantSaved {
			t.Fatalf("%s: saved = %v, want %v", step.name, saved, step.wantSaved)
		}
	}

	v, err := s.Robot(ctx, "robot-1")
	if err != nil {
		t.Fatal(err)
	}
	if v.State.Power.StateOfCharge != 20 || !v.State.ReportedAt.Equal(base.Add(3*time.Second)) {
		t.Fatalf("state = %+v, want the newest report", v.State)
	}
	if len(v.State.Faults) != 1 || !v.State.Faults[0].Level.Severe() {
		t.Fatalf("faults = %+v, want the critical fault back", v.State.Faults)
	}
}

func TestAppendSkipsDuplicates(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	first := record("robot-1", base, 50)

	inserted, err := s.Append(ctx, []telemetry.Record{first, record("robot-1", base.Add(time.Second), 49), first})
	if err != nil {
		t.Fatal(err)
	}
	if want := []bool{true, true, false}; !slices.Equal(inserted, want) {
		t.Fatalf("inserted = %v, want %v (duplicate inside the batch)", inserted, want)
	}

	inserted, err = s.Append(ctx, []telemetry.Record{first})
	if err != nil || inserted[0] {
		t.Fatalf("re-sent record: inserted = %v, err = %v; want a duplicate", inserted, err)
	}

	history, err := s.History(ctx, "robot-1", base, base.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || !history[0].ReportedAt.Equal(base) {
		t.Fatalf("history = %+v, want 2 points, oldest first", history)
	}
}

func TestLookup(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	id, err := s.Lookup(ctx, telemetry.Identity{Manufacturer: "acme", SerialNumber: "CLN-0043"})
	if err != nil || id != "robot-2" {
		t.Fatalf("got %q, %v; want robot-2", id, err)
	}
	_, err = s.Lookup(ctx, telemetry.Identity{Manufacturer: "acme", SerialNumber: "NOPE"})
	if !errors.Is(err, telemetry.ErrUnknownRobot) {
		t.Fatalf("err = %v, want ErrUnknownRobot", err)
	}
}

func TestReaderIncludesRobotsThatNeverReported(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.SaveIfNewer(ctx, record("robot-1", base, 50)); err != nil {
		t.Fatal(err)
	}

	views, err := s.Robots(ctx, "hq")
	if err != nil {
		t.Fatal(err)
	}
	states := map[telemetry.RobotID]*fleet.State{}
	for _, v := range views {
		states[v.Robot.ID] = v.State
	}
	if len(views) != 2 || states["robot-1"] == nil || states["robot-2"] != nil {
		t.Fatalf("views = %+v, want robot-1 with state and robot-2 without", views)
	}
	if _, err := s.Robot(ctx, "robot-9"); !errors.Is(err, fleet.ErrRobotNotFound) {
		t.Fatalf("err = %v, want ErrRobotNotFound", err)
	}
}
