// Command simulator plays a gateway in front of a fleet: it discovers the
// registered robots through the API and sends their state as one batch per
// tick. It also injects what real networks and robots do: duplicates, late
// reports, an unregistered robot and a skewed clock.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/underson/robot-fleet/backend/internal/adapters/vda"
)

type Config struct {
	APIURL   string        `env:"API_URL" envDefault:"http://localhost:8080"`
	Interval time.Duration `env:"INTERVAL" envDefault:"1s"`
	Seed     uint64        `env:"SEED" envDefault:"42"` // same seed, same run

	// Chance per tick of each network fault.
	DuplicateChance float64 `env:"DUPLICATE_CHANCE" envDefault:"0.05"`
	LateChance      float64 `env:"LATE_CHANCE" envDefault:"0.05"`
	ClockSkewChance float64 `env:"CLOCK_SKEW_CHANCE" envDefault:"0.01"`
	UnknownEvery    int     `env:"UNKNOWN_EVERY" envDefault:"30"` // ticks between unregistered-robot messages
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("simulator stopped", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	client := &apiClient{baseURL: cfg.APIURL, http: &http.Client{Timeout: 5 * time.Second}}
	rng := rand.New(rand.NewPCG(cfg.Seed, cfg.Seed))

	registered, err := client.waitForRobots(ctx, logger)
	if err != nil {
		return err
	}
	// The API lists the most urgent robots first, so that order depends on the
	// fleet's current health. Sort by serial number so each robot gets the same
	// role on every run.
	slices.SortFunc(registered, func(a, b robotInfo) int { return strings.Compare(a.SerialNumber, b.SerialNumber) })

	now := time.Now()
	robots := make([]*Robot, 0, len(registered))
	for robotIdx, info := range registered {
		role := RoleNormal
		if robotIdx < len(specialRoles) {
			role = specialRoles[robotIdx]
		}
		robots = append(robots, NewRobot(info.Manufacturer, info.SerialNumber, info.Site.ID, role, rng, now))
		logger.Info("simulating robot", "serial", info.SerialNumber, "site", info.Site.ID, "role", role)
	}

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	var previous []vda.StateMessage // last tick's batch, source for duplicates and late reports
	var totals ingestTotals
	for tick := 1; ; tick++ {
		select {
		case <-ctx.Done():
			return nil
		case now = <-ticker.C:
		}

		batch := make([]vda.StateMessage, 0, len(robots)+3)
		for _, robot := range robots {
			robot.Step(cfg.Interval)
			if !robot.Silent(now) {
				batch = append(batch, robot.Message(now))
			}
		}
		batch = append(batch, networkFaults(cfg, rng, tick, now, batch, previous)...)
		previous = batch

		resp, err := client.send(ctx, batch)
		if err != nil {
			// A real gateway would buffer and retry; the next tick sends fresh state.
			logger.Warn("send failed", "err", err)
			continue
		}
		totals.add(resp)
		if tick%10 == 0 {
			logger.Info("progress", "ticks", tick, "statuses", totals.byStatus, "rejections", totals.byReason)
		}
	}
}

// networkFaults returns extra messages for this tick: a re-sent message from
// the last tick (duplicate), last tick's message for a robot that already
// reported newer state (late), a robot nobody registered, and a clock running
// an hour ahead.
func networkFaults(cfg Config, rng *rand.Rand, tick int, now time.Time, current, previous []vda.StateMessage) []vda.StateMessage {
	var extra []vda.StateMessage
	if len(previous) > 0 && rng.Float64() < cfg.DuplicateChance {
		extra = append(extra, previous[rng.IntN(len(previous))])
	}
	if len(previous) > 0 && rng.Float64() < cfg.LateChance {
		late := previous[rng.IntN(len(previous))]
		late.Timestamp = late.Timestamp.Add(-500 * time.Millisecond) // older than anything stored
		extra = append(extra, late)
	}
	if len(current) > 0 && rng.Float64() < cfg.ClockSkewChance {
		skewed := current[rng.IntN(len(current))]
		skewed.Timestamp = now.Add(time.Hour).UTC()
		extra = append(extra, skewed)
	}
	if cfg.UnknownEvery > 0 && tick%cfg.UnknownEvery == 0 && len(current) > 0 {
		unknown := current[0]
		unknown.Manufacturer, unknown.SerialNumber = "acme", "UNREGISTERED-0001"
		extra = append(extra, unknown)
	}
	return extra
}

type robotInfo struct {
	Manufacturer string `json:"manufacturer"`
	SerialNumber string `json:"serialNumber"`
	Site         struct {
		ID string `json:"id"`
	} `json:"site"`
}

type ingestResponse struct {
	Results []struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"results"`
}

type ingestTotals struct {
	byStatus map[string]int
	byReason map[string]int
}

func (t *ingestTotals) add(resp ingestResponse) {
	if t.byStatus == nil {
		t.byStatus, t.byReason = map[string]int{}, map[string]int{}
	}
	for _, result := range resp.Results {
		t.byStatus[result.Status]++
		if result.Reason != "" {
			t.byReason[result.Reason]++
		}
	}
}

type apiClient struct {
	baseURL string
	http    *http.Client
}

// waitForRobots polls until the API is up and has robots: in compose, the
// simulator may start before fleetd has migrated and seeded.
func (c *apiClient) waitForRobots(ctx context.Context, logger *slog.Logger) ([]robotInfo, error) {
	for {
		robots, err := c.robots(ctx)
		if err == nil && len(robots) > 0 {
			return robots, nil
		}
		logger.Info("waiting for fleetd and registered robots", "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// robots reads every page of the fleet overview.
func (c *apiClient) robots(ctx context.Context) ([]robotInfo, error) {
	const pageSize = 200
	var all []robotInfo
	for offset := 0; ; offset += pageSize {
		var page struct {
			Robots []robotInfo `json:"robots"`
			Total  int         `json:"total"`
		}
		url := fmt.Sprintf("%s/v1/fleet?offset=%d&limit=%d", c.baseURL, offset, pageSize)
		if err := c.do(ctx, http.MethodGet, url, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Robots...)
		if offset+pageSize >= page.Total {
			return all, nil
		}
	}
}

func (c *apiClient) send(ctx context.Context, batch []vda.StateMessage) (ingestResponse, error) {
	body, err := json.Marshal(batch)
	if err != nil {
		return ingestResponse{}, err
	}
	var resp ingestResponse
	err = c.do(ctx, http.MethodPost, c.baseURL+"/v1/telemetry", body, &resp)
	return resp, err
}

func (c *apiClient) do(ctx context.Context, method, url string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(method + " " + url + ": " + resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
