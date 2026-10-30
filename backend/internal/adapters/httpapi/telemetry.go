package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/underson/robot-fleet/backend/internal/adapters/vda"
	"github.com/underson/robot-fleet/backend/internal/domain/telemetry"
)

const (
	maxBatchBytes   = 1 << 20 // 1 MiB
	maxBatchReports = 1000
)

type ingestResponse struct {
	Accepted int            `json:"accepted"`
	Rejected int            `json:"rejected"`
	Results  []ingestResult `json:"results"` // same order as the request
}

type ingestResult struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// postTelemetry accepts a JSON array of state messages. Each message gets
// its own result: one bad message never fails the batch. 5xx means storage
// failed and the whole batch can be retried safely.
func (s *Server) postTelemetry(w http.ResponseWriter, r *http.Request) {
	var raw []json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBatchBytes)).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "body must be a JSON array of state messages: "+err.Error())
		return
	}
	if len(raw) > maxBatchReports {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("at most %d messages per batch", maxBatchReports))
		return
	}

	// Translate each message; only translated ones go to the use case.
	results := make([]ingestResult, len(raw))
	var reports []telemetry.Report
	var requestIndex []int // requestIndex[reportIdx] is that report's position in the request
	for msgIdx, msg := range raw {
		report, err := vda.Decode(msg)
		if err != nil {
			results[msgIdx] = rejectedResult(telemetry.ReasonInvalid, err)
			continue
		}
		reports = append(reports, report)
		requestIndex = append(requestIndex, msgIdx)
	}

	outcomes, err := s.ingester.Ingest(r.Context(), reports)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	for reportIdx, outcome := range outcomes {
		results[requestIndex[reportIdx]] = ingestResult{Status: string(outcome.Status), Reason: string(outcome.Reason), Detail: outcome.Detail}
	}

	resp := ingestResponse{Results: results}
	for _, result := range results {
		if result.Status != string(telemetry.StatusRejected) {
			resp.Accepted++
			continue
		}
		resp.Rejected++
		rejections.Add(result.Reason, 1)
		s.logger.Warn("telemetry rejected", "reason", result.Reason, "detail", result.Detail)
	}
	writeJSON(w, http.StatusOK, resp)
}

func rejectedResult(reason telemetry.Reason, err error) ingestResult {
	return ingestResult{Status: string(telemetry.StatusRejected), Reason: string(reason), Detail: err.Error()}
}
