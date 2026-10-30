package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/underson/robot-fleet/backend/internal/domain/fleet"
	"github.com/underson/robot-fleet/backend/internal/domain/telemetry"
)

const (
	defaultPageSize = 50
	maxPageSize     = 200
)

// Response types are the API contract (see openapi.yaml). They are separate
// from domain types so the domain can change without breaking clients.

type siteJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type overviewJSON struct {
	Summary summaryJSON       `json:"summary"`
	Robots  []robotStatusJSON `json:"robots"`
	Total   int               `json:"total"`
	Offset  int               `json:"offset"`
	Limit   int               `json:"limit"`
}

type summaryJSON struct {
	Total          int `json:"total"`
	Online         int `json:"online"`
	Offline        int `json:"offline"`
	NeverReported  int `json:"neverReported"`
	NeedsAttention int `json:"needsAttention"`
	LowBattery     int `json:"lowBattery"`
}

type robotStatusJSON struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Site         siteJSON   `json:"site"`
	Manufacturer string     `json:"manufacturer"`
	SerialNumber string     `json:"serialNumber"`
	Model        string     `json:"model"`
	Health       healthJSON `json:"health"`
	State        *stateJSON `json:"state"` // null if the robot never reported
}

type healthJSON struct {
	Connectivity  string   `json:"connectivity"`
	Battery       string   `json:"battery"`
	PositionKnown bool     `json:"positionKnown"`
	Attention     []string `json:"attention"` // most urgent first
}

type stateJSON struct {
	ReportedAt time.Time    `json:"reportedAt"`
	ReceivedAt time.Time    `json:"receivedAt"`
	Position   positionJSON `json:"position"`
	Power      powerJSON    `json:"power"`
	Mode       string       `json:"mode"`
	Faults     []faultJSON  `json:"faults"`
	Safety     safetyJSON   `json:"safety"`
}

type positionJSON struct {
	MapID     string  `json:"mapId"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Theta     float64 `json:"theta"`
	Localized bool    `json:"localized"`
}

type powerJSON struct {
	StateOfCharge float64 `json:"stateOfCharge"`
	Charging      bool    `json:"charging"`
}

type faultJSON struct {
	Type        string `json:"type"`
	Level       string `json:"level"`
	Description string `json:"description,omitempty"`
}

type safetyJSON struct {
	EmergencyStop  string `json:"emergencyStop"`
	FieldViolation bool   `json:"fieldViolation"`
}

type robotDetailJSON struct {
	robotStatusJSON
	History []historyPointJSON `json:"history"` // oldest first
}

type historyPointJSON struct {
	ReportedAt time.Time    `json:"reportedAt"`
	Position   positionJSON `json:"position"`
	Power      powerJSON    `json:"power"`
	Mode       string       `json:"mode"`
	Faults     []faultJSON  `json:"faults"`
}

func (s *Server) getSites(w http.ResponseWriter, r *http.Request) {
	sites, err := s.fleet.Sites(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]siteJSON, 0, len(sites))
	for _, site := range sites {
		out = append(out, siteJSON{ID: site.ID, Name: site.Name})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getFleet(w http.ResponseWriter, r *http.Request) {
	query, err := parseOverviewQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	overview, err := s.fleet.Overview(r.Context(), query)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	summary := overview.Summary
	out := overviewJSON{
		Summary: summaryJSON{
			Total: summary.Total, Online: summary.Online, Offline: summary.Offline, NeverReported: summary.NeverReported,
			NeedsAttention: summary.NeedsAttention, LowBattery: summary.LowBattery,
		},
		Robots: make([]robotStatusJSON, 0, len(overview.Robots)),
		Total:  overview.Total,
		Offset: query.Offset,
		Limit:  query.Limit,
	}
	for _, robotStatus := range overview.Robots {
		out.Robots = append(out.Robots, toRobotStatusJSON(robotStatus))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getRobot(w http.ResponseWriter, r *http.Request) {
	detail, err := s.fleet.Robot(r.Context(), telemetry.RobotID(r.PathValue("id")))
	if errors.Is(err, fleet.ErrRobotNotFound) {
		writeError(w, http.StatusNotFound, "robot not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := robotDetailJSON{robotStatusJSON: toRobotStatusJSON(detail.RobotStatus), History: make([]historyPointJSON, 0, len(detail.History))}
	for _, point := range detail.History {
		out.History = append(out.History, historyPointJSON{
			ReportedAt: point.ReportedAt, Position: toPositionJSON(point.Position), Power: toPowerJSON(point.Power),
			Mode: string(point.Mode), Faults: toFaultsJSON(point.Faults),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func parseOverviewQuery(r *http.Request) (fleet.OverviewQuery, error) {
	params := r.URL.Query()
	query := fleet.OverviewQuery{SiteID: params.Get("site"), Limit: defaultPageSize}

	switch status := fleet.StatusFilter(params.Get("status")); status {
	case fleet.StatusAll, fleet.StatusAttention, fleet.StatusOnline, fleet.StatusOffline:
		query.Status = status
	default:
		return query, errors.New("status must be one of: attention, online, offline")
	}
	var err error
	if query.Offset, err = intParam(params.Get("offset"), 0, 0, -1); err != nil {
		return query, errors.New("offset must be a non-negative integer")
	}
	if query.Limit, err = intParam(params.Get("limit"), defaultPageSize, 1, maxPageSize); err != nil {
		return query, errors.New("limit must be between 1 and " + strconv.Itoa(maxPageSize))
	}
	return query, nil
}

// intParam parses an optional integer in [lowest, highest]; highest < 0 means no upper bound.
func intParam(value string, fallback, lowest, highest int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < lowest || (highest >= 0 && parsed > highest) {
		return 0, errors.New("out of range")
	}
	return parsed, nil
}

func toRobotStatusJSON(robotStatus fleet.RobotStatus) robotStatusJSON {
	robot, health := robotStatus.Robot, robotStatus.Health
	out := robotStatusJSON{
		ID: string(robot.ID), Name: robot.Name, Site: siteJSON{ID: robot.Site.ID, Name: robot.Site.Name},
		Manufacturer: robot.Manufacturer, SerialNumber: robot.SerialNumber, Model: robot.Model,
		Health: healthJSON{
			Connectivity: string(health.Connectivity), Battery: string(health.Battery),
			PositionKnown: health.PositionKnown, Attention: make([]string, 0, len(health.Attention)),
		},
	}
	for _, reason := range health.Attention {
		out.Health.Attention = append(out.Health.Attention, string(reason))
	}
	if state := robotStatus.State; state != nil {
		out.State = &stateJSON{
			ReportedAt: state.ReportedAt, ReceivedAt: state.ReceivedAt,
			Position: toPositionJSON(state.Position), Power: toPowerJSON(state.Power),
			Mode: string(state.Mode), Faults: toFaultsJSON(state.Faults),
			Safety: safetyJSON{EmergencyStop: string(state.Safety.EmergencyStop), FieldViolation: state.Safety.FieldViolation},
		}
	}
	return out
}

func toPositionJSON(position telemetry.Position) positionJSON {
	return positionJSON{MapID: position.MapID, X: position.X, Y: position.Y, Theta: position.Theta, Localized: position.Localized}
}

func toPowerJSON(power telemetry.Power) powerJSON {
	return powerJSON{StateOfCharge: power.StateOfCharge, Charging: power.Charging}
}

func toFaultsJSON(faults []telemetry.Fault) []faultJSON {
	out := make([]faultJSON, 0, len(faults)) // [] not null in JSON
	for _, fault := range faults {
		out = append(out, faultJSON{Type: fault.Type, Level: string(fault.Level), Description: fault.Description})
	}
	return out
}
