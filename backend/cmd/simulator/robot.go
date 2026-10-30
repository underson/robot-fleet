package main

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/underson/robot-fleet/backend/internal/adapters/vda"
)

// Role is a robot's scripted behaviour, so every health rule shows up in the
// running system instead of only in tests.
type Role string

const (
	RoleNormal           Role = "normal"            // works, drains, goes to charge at low battery
	RoleEmergencyStop    Role = "emergency_stop"    // e-stop for 40 s every 90 s
	RoleFaulty           Role = "faulty"            // critical fault for 30 s every 75 s
	RoleSilent           Role = "silent"            // stops reporting for 40 s every 2 min
	RoleStranded         Role = "stranded"          // drains to near empty and never charges
	RoleChargingCritical Role = "charging_critical" // starts nearly empty, on its charger
	RoleLost             Role = "lost"              // loses localization for 20 s every minute
)

// specialRoles go to the first robots of the fleet; the rest are normal.
var specialRoles = []Role{RoleEmergencyStop, RoleFaulty, RoleSilent, RoleStranded, RoleChargingCritical, RoleLost}

const (
	mapWidth, mapHeight = 60.0, 40.0 // metres
	speed               = 0.8        // metres per second
	drainPerSecond      = 0.15       // percent while working
	chargePerSecond     = 0.6        // percent while charging
	goChargeBelow       = 18.0       // percent
	chargedAbove        = 95.0       // percent
)

type Robot struct {
	Manufacturer string
	SerialNumber string
	MapID        string
	Role         Role

	x, y, theta   float64
	stateOfCharge float64
	charging      bool
	startedAt     time.Time
	rng           *rand.Rand
}

func NewRobot(manufacturer, serialNumber, siteID string, role Role, rng *rand.Rand, now time.Time) *Robot {
	robot := &Robot{
		Manufacturer: manufacturer, SerialNumber: serialNumber, MapID: siteID + "-floor-1", Role: role,
		x: rng.Float64() * mapWidth, y: rng.Float64() * mapHeight, theta: (rng.Float64()*2 - 1) * math.Pi,
		stateOfCharge: 40 + rng.Float64()*60,
		startedAt:     now, rng: rng,
	}
	switch role {
	case RoleStranded:
		robot.stateOfCharge = 14 // critical (≤ 10 %) after about 30 s
	case RoleChargingCritical:
		robot.stateOfCharge, robot.charging = 6, true
	}
	return robot
}

// Step advances the robot by elapsed time: it moves while working and drains
// or charges its battery.
func (r *Robot) Step(elapsed time.Duration) {
	seconds := elapsed.Seconds()
	if r.charging {
		rate := chargePerSecond
		if r.Role == RoleChargingCritical {
			rate /= 10 // a slow charger, so it stays critical (but fine) for over a minute
		}
		r.stateOfCharge = math.Min(100, r.stateOfCharge+rate*seconds)
		if r.stateOfCharge >= chargedAbove && r.Role != RoleChargingCritical {
			r.charging = false
		}
		return
	}

	r.stateOfCharge = math.Max(0, r.stateOfCharge-drainPerSecond*seconds)
	if r.stateOfCharge < goChargeBelow && r.Role != RoleStranded {
		r.charging = true
		return
	}
	if r.Role == RoleStranded && r.stateOfCharge < 5 {
		return // out of power: stands still
	}
	r.move(seconds)
}

// move drives in a straight line, turns a little at random, and bounces off
// the map's edges.
func (r *Robot) move(seconds float64) {
	r.theta += (r.rng.Float64() - 0.5) * 0.3
	r.x += math.Cos(r.theta) * speed * seconds
	r.y += math.Sin(r.theta) * speed * seconds
	if r.x < 0 || r.x > mapWidth {
		r.x = math.Max(0, math.Min(mapWidth, r.x))
		r.theta = math.Pi - r.theta
	}
	if r.y < 0 || r.y > mapHeight {
		r.y = math.Max(0, math.Min(mapHeight, r.y))
		r.theta = -r.theta
	}
	r.theta = math.Atan2(math.Sin(r.theta), math.Cos(r.theta)) // keep in [-π, π]
}

// Silent reports whether the robot sends nothing right now.
func (r *Robot) Silent(now time.Time) bool {
	return r.Role == RoleSilent && r.inWindow(now, 2*time.Minute, 40*time.Second)
}

// Message is the robot's current state in the wire format.
func (r *Robot) Message(now time.Time) vda.StateMessage {
	mode := "AUTOMATIC"
	emergencyStop := "NONE"
	errs := []vda.Error{}

	if r.Role == RoleEmergencyStop && r.inWindow(now, 90*time.Second, 40*time.Second) {
		emergencyStop, mode = "REMOTE", "INTERVENED"
	}
	if r.Role == RoleFaulty && r.inWindow(now, 75*time.Second, 30*time.Second) {
		errs = append(errs, vda.Error{ErrorType: "motorOverheat", ErrorLevel: "CRITICAL", ErrorDescription: "Drive motor above 90 °C"})
	}
	if r.stateOfCharge < goChargeBelow && !r.charging {
		errs = append(errs, vda.Error{ErrorType: "batteryLow", ErrorLevel: "WARNING"})
	}
	localized := !(r.Role == RoleLost && r.inWindow(now, time.Minute, 20*time.Second))

	return vda.StateMessage{
		Manufacturer:  r.Manufacturer,
		SerialNumber:  r.SerialNumber,
		Timestamp:     now.UTC(),
		Position:      &vda.Position{MapID: r.MapID, X: round(r.x), Y: round(r.y), Theta: round(r.theta), Localized: localized},
		PowerSupply:   &vda.PowerSupply{StateOfCharge: round(r.stateOfCharge), Charging: r.charging},
		OperatingMode: mode,
		Errors:        errs,
		SafetyState:   &vda.SafetyState{ActiveEmergencyStop: emergencyStop},
	}
}

// warmup gives every robot a few normal reports before scripted events start,
// so the console first shows a healthy fleet.
const warmup = 5 * time.Second

// inWindow is true during the first `active` part of every `period` after the
// warmup, so each scripted event shows up early and then repeats.
func (r *Robot) inWindow(now time.Time, period, active time.Duration) bool {
	elapsed := now.Sub(r.startedAt) - warmup
	return elapsed >= 0 && elapsed%period < active
}

func round(value float64) float64 {
	return math.Round(value*100) / 100
}
