package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/underson/robot-fleet/backend/internal/domain/fleet"
	"github.com/underson/robot-fleet/backend/internal/domain/telemetry"
)

// fakeIngester applies every report and records what it was given.
type fakeIngester struct {
	got []telemetry.Report
	err error
}

func (f *fakeIngester) Ingest(_ context.Context, reports []telemetry.Report) ([]telemetry.Outcome, error) {
	f.got = reports
	if f.err != nil {
		return nil, f.err
	}
	outcomes := make([]telemetry.Outcome, 0, len(reports))
	for range reports {
		outcomes = append(outcomes, telemetry.Outcome{Status: telemetry.StatusApplied})
	}
	return outcomes, nil
}

type fakeFleet struct {
	query fleet.OverviewQuery
}

func (f *fakeFleet) Sites(context.Context) ([]fleet.Site, error) { return nil, nil }

func (f *fakeFleet) Overview(_ context.Context, query fleet.OverviewQuery) (fleet.Overview, error) {
	f.query = query
	return fleet.Overview{}, nil
}

func (f *fakeFleet) Robot(_ context.Context, id telemetry.RobotID) (fleet.RobotDetail, error) {
	if id != "robot-1" {
		return fleet.RobotDetail{}, fleet.ErrRobotNotFound
	}
	return fleet.RobotDetail{RobotStatus: fleet.RobotStatus{RobotView: fleet.RobotView{Robot: fleet.Robot{ID: id}}}}, nil
}

type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

func newTestServer() (*Server, *fakeIngester, *fakeFleet) {
	ingester, fleetFake := &fakeIngester{}, &fakeFleet{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewServer(ingester, fleetFake, okPinger{}, logger), ingester, fleetFake
}

func do(t *testing.T, server *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, httptest.NewRequest(method, path, strings.NewReader(body)))
	return recorder
}

const validMessage = `{"manufacturer":"acme","serialNumber":"CLN-0042","timestamp":"2026-10-30T09:15:02Z",
	"powerSupply":{"stateOfCharge":50,"charging":false},"operatingMode":"AUTOMATIC","errors":[],
	"safetyState":{"activeEmergencyStop":"NONE","fieldViolation":false}}`

func TestPostTelemetryKeepsResultsInRequestOrder(t *testing.T) {
	server, ingester, _ := newTestServer()

	// The middle message can't be translated; the others reach the use case.
	recorder := do(t, server, http.MethodPost, "/v1/telemetry", `[`+validMessage+`, {"timestamp":"yesterday"}, `+validMessage+`]`)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body)
	}
	var resp ingestResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	statuses := []string{resp.Results[0].Status, resp.Results[1].Status, resp.Results[2].Status}
	if strings.Join(statuses, ",") != "applied,rejected,applied" {
		t.Fatalf("statuses = %v", statuses)
	}
	if resp.Results[1].Reason != "invalid" || resp.Accepted != 2 || resp.Rejected != 1 {
		t.Fatalf("resp = %+v", resp)
	}
	if len(ingester.got) != 2 {
		t.Fatalf("use case got %d reports, want 2", len(ingester.got))
	}
}

func TestPostTelemetryRejectsNonArrayBody(t *testing.T) {
	server, _, _ := newTestServer()
	if recorder := do(t, server, http.MethodPost, "/v1/telemetry", validMessage); recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestPostTelemetryStorageFailureIs500(t *testing.T) {
	server, ingester, _ := newTestServer()
	ingester.err = errors.New("connection refused")

	recorder := do(t, server, http.MethodPost, "/v1/telemetry", `[`+validMessage+`]`)

	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "connection refused") {
		t.Fatalf("status = %d, body %s; want 500 without internal details", recorder.Code, recorder.Body)
	}
}

func TestGetFleetParsesQuery(t *testing.T) {
	server, _, fleetFake := newTestServer()

	recorder := do(t, server, http.MethodGet, "/v1/fleet?site=muc-office&status=attention&offset=50&limit=25", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	want := fleet.OverviewQuery{SiteID: "muc-office", Status: fleet.StatusAttention, Offset: 50, Limit: 25}
	if fleetFake.query != want {
		t.Fatalf("query = %+v, want %+v", fleetFake.query, want)
	}
}

func TestGetFleetRejectsBadQuery(t *testing.T) {
	for _, query := range []string{"status=sleeping", "limit=0", "limit=500", "offset=-1", "offset=abc"} {
		server, _, _ := newTestServer()
		if recorder := do(t, server, http.MethodGet, "/v1/fleet?"+query, ""); recorder.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", query, recorder.Code)
		}
	}
}

func TestGetRobot(t *testing.T) {
	server, _, _ := newTestServer()
	if recorder := do(t, server, http.MethodGet, "/v1/robots/robot-1", ""); recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if recorder := do(t, server, http.MethodGet, "/v1/robots/nope", ""); recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}
