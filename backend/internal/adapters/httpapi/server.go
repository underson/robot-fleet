// Package httpapi exposes the use cases over HTTP. It maps requests to domain
// calls and domain results to JSON; it holds no business rules.
package httpapi

import (
	"context"
	_ "embed"
	"encoding/json"
	"expvar"
	"log/slog"
	"net/http"

	"github.com/underson/robot-fleet/backend/internal/domain/fleet"
	"github.com/underson/robot-fleet/backend/internal/domain/telemetry"
)

//go:embed openapi.yaml
var openAPISpec []byte

// The handlers depend on these small interfaces, not on concrete use cases,
// so they can be tested without a database.
type Ingester interface {
	Ingest(ctx context.Context, reports []telemetry.Report) ([]telemetry.Outcome, error)
}

type Fleet interface {
	Sites(ctx context.Context) ([]fleet.Site, error)
	Overview(ctx context.Context, q fleet.OverviewQuery) (fleet.Overview, error)
	Robot(ctx context.Context, id telemetry.RobotID) (fleet.RobotDetail, error)
}

type Pinger interface {
	Ping(ctx context.Context) error
}

// rejections counts rejected reports per reason; served at /debug/vars.
var rejections = expvar.NewMap("telemetry_rejected_total")

type Server struct {
	ingester Ingester
	fleet    Fleet
	db       Pinger
	logger   *slog.Logger
}

func NewServer(ingester Ingester, fleet Fleet, db Pinger, logger *slog.Logger) *Server {
	return &Server{ingester: ingester, fleet: fleet, db: db, logger: logger}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/telemetry", s.postTelemetry)
	mux.HandleFunc("GET /v1/sites", s.getSites)
	mux.HandleFunc("GET /v1/fleet", s.getFleet)
	mux.HandleFunc("GET /v1/robots/{id}", s.getRobot)

	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(openAPISpec)
	})
	// Liveness: the process is up. Readiness: it can reach its database.
	// A database outage should stop traffic, not restart every instance.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.db.Ping(r.Context()); err != nil {
			writeError(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /debug/vars", expvar.Handler())
	return mux
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// internalError logs the cause and returns a generic message: internal
// details stay in the logs, not in responses.
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
