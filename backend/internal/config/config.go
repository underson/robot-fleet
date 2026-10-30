// Package config reads service configuration from environment variables only,
// so the same image runs in the cloud and on a customer's servers.
package config

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/underson/robot-fleet/backend/internal/domain/fleet"
)

// Config lists every setting with its variable and default. The tags are the
// documentation.
type Config struct {
	HTTPAddr        string        `env:"HTTP_ADDR" envDefault:":8080"`
	DatabaseURL     string        `env:"DATABASE_URL,required,notEmpty"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`
	SeedDemo        bool          `env:"SEED_DEMO" envDefault:"false"`

	// How far ahead of server time a report may be stamped.
	MaxClockSkew time.Duration `env:"MAX_CLOCK_SKEW" envDefault:"5s"`
	// How far back the robot detail view looks.
	HistoryWindow time.Duration `env:"HISTORY_WINDOW" envDefault:"10m"`

	// Health thresholds; see fleet.HealthPolicy.
	OfflineAfter           time.Duration `env:"OFFLINE_AFTER" envDefault:"10s"`
	LowBatteryPercent      float64       `env:"LOW_BATTERY_PERCENT" envDefault:"20"`
	CriticalBatteryPercent float64       `env:"CRITICAL_BATTERY_PERCENT" envDefault:"10"`
}

// Load parses the environment and checks the values. Every problem is reported
// at once, so a misconfigured install is fixed in one go, not one per restart.
func Load() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", withVariableNames(err))
	}
	if err := cfg.validate(); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

// HealthPolicy builds the domain policy. The domain type has no env tags: it
// doesn't know where its values come from.
func (c Config) HealthPolicy() fleet.HealthPolicy {
	return fleet.HealthPolicy{
		OfflineAfter:    c.OfflineAfter,
		LowBattery:      c.LowBatteryPercent,
		CriticalBattery: c.CriticalBatteryPercent,
	}
}

func (c Config) validate() error {
	// Slices, not maps, so problems are always reported in the same order.
	durations := []struct {
		variable string
		value    time.Duration
	}{
		{"SHUTDOWN_TIMEOUT", c.ShutdownTimeout},
		{"MAX_CLOCK_SKEW", c.MaxClockSkew},
		{"HISTORY_WINDOW", c.HistoryWindow},
		{"OFFLINE_AFTER", c.OfflineAfter},
	}
	percents := []struct {
		variable string
		value    float64
	}{
		{"LOW_BATTERY_PERCENT", c.LowBatteryPercent},
		{"CRITICAL_BATTERY_PERCENT", c.CriticalBatteryPercent},
	}

	var problems []error
	for _, duration := range durations {
		if duration.value <= 0 {
			problems = append(problems, fmt.Errorf("%s must be positive", duration.variable))
		}
	}
	for _, percent := range percents {
		if percent.value < 0 || percent.value > 100 {
			problems = append(problems, fmt.Errorf("%s must be between 0 and 100", percent.variable))
		}
	}
	if c.CriticalBatteryPercent > c.LowBatteryPercent {
		problems = append(problems, errors.New("CRITICAL_BATTERY_PERCENT must not exceed LOW_BATTERY_PERCENT"))
	}
	return errors.Join(problems...)
}

// withVariableNames rewrites parse errors to name the environment variable
// (MAX_CLOCK_SKEW) instead of the Go field (MaxClockSkew): the operator
// setting it never sees the code.
func withVariableNames(err error) error {
	var aggregate env.AggregateError
	if !errors.As(err, &aggregate) {
		return err
	}
	configType := reflect.TypeFor[Config]()
	problems := make([]error, 0, len(aggregate.Errors))
	for _, problem := range aggregate.Errors {
		var parseErr env.ParseError
		if !errors.As(problem, &parseErr) {
			problems = append(problems, problem)
			continue
		}
		field, _ := configType.FieldByName(parseErr.Name)
		variable, _, _ := strings.Cut(field.Tag.Get("env"), ",")
		problems = append(problems, fmt.Errorf("%s: %w", variable, parseErr.Err))
	}
	return errors.Join(problems...)
}
