package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/fleet")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.MaxClockSkew != 5*time.Second || cfg.HistoryWindow != 10*time.Minute || cfg.SeedDemo {
		t.Fatalf("cfg = %+v", cfg)
	}
	policy := cfg.HealthPolicy()
	if policy.OfflineAfter != 10*time.Second || policy.LowBattery != 20 || policy.CriticalBattery != 10 {
		t.Fatalf("policy = %+v", policy)
	}
}

func TestLoadReadsVariables(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/fleet")
	t.Setenv("OFFLINE_AFTER", "45s")
	t.Setenv("SEED_DEMO", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OfflineAfter != 45*time.Second || !cfg.SeedDemo {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadReportsAllParseErrors(t *testing.T) {
	t.Setenv("DATABASE_URL", "") // set but empty counts as missing
	t.Setenv("MAX_CLOCK_SKEW", "soon")
	t.Setenv("LOW_BATTERY_PERCENT", "lots")

	_, err := Load()
	if err == nil {
		t.Fatal("want error")
	}
	for _, want := range []string{"DATABASE_URL", "MAX_CLOCK_SKEW", "LOW_BATTERY_PERCENT"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s: %v", want, err)
		}
	}
}

func TestLoadReportsAllRangeErrors(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/fleet")
	t.Setenv("OFFLINE_AFTER", "-1s")
	t.Setenv("LOW_BATTERY_PERCENT", "5")
	t.Setenv("CRITICAL_BATTERY_PERCENT", "150")

	_, err := Load()
	if err == nil {
		t.Fatal("want error")
	}
	for _, want := range []string{"OFFLINE_AFTER must be positive", "CRITICAL_BATTERY_PERCENT must be between", "must not exceed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should contain %q: %v", want, err)
		}
	}
}
