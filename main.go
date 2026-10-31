package main

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"
	corev2 "github.com/sensu/core/v2"
	"github.com/sensu/sensu-plugin-sdk/sensu"
)

// Config represents the check plugin config.
type Config struct {
	sensu.PluginConfig
	Units   []string
	Exclude []string
	Types   []string
	Timeout int
}

var (
	plugin = Config{
		PluginConfig: sensu.PluginConfig{
			Name:     "check-systemd",
			Short:    "Alert on failed or restart-looping systemd units",
			Keyspace: "sensu.io/plugins/check-systemd/config",
		},
	}

	options = []sensu.ConfigOption{
		&sensu.SlicePluginConfigOption[string]{
			Path:      "unit",
			Env:       "CHECK_SYSTEMD_UNITS",
			Argument:  "unit",
			Shorthand: "u",
			Default:   []string{},
			Usage:     "Unit that must be active (repeatable, e.g. -u nginx.service -u certbot.timer)",
			Value:     &plugin.Units,
		},
		&sensu.SlicePluginConfigOption[string]{
			Path:      "exclude",
			Env:       "CHECK_SYSTEMD_EXCLUDE",
			Argument:  "exclude",
			Shorthand: "e",
			Default:   []string{},
			Usage:     "Glob pattern of units to ignore (repeatable, e.g. -e 'user@*.service')",
			Value:     &plugin.Exclude,
		},
		&sensu.SlicePluginConfigOption[string]{
			Path:      "type",
			Env:       "CHECK_SYSTEMD_TYPES",
			Argument:  "type",
			Shorthand: "t",
			Default:   []string{},
			Usage:     "Unit types to inspect for failures (repeatable, e.g. -t service -t timer). Default: all",
			Value:     &plugin.Types,
		},
		&sensu.PluginConfigOption[int]{
			Path:     "timeout",
			Env:      "CHECK_SYSTEMD_TIMEOUT",
			Argument: "timeout",
			Default:  10,
			Usage:    "D-Bus query timeout in seconds",
			Value:    &plugin.Timeout,
		},
	}
)

func main() {
	check := sensu.NewGoCheck(&plugin.PluginConfig, options, checkArgs, executeCheck, false)
	check.Execute()
}

func checkArgs(event *corev2.Event) (int, error) {
	if plugin.Timeout <= 0 {
		return sensu.CheckStateUnknown, fmt.Errorf("--timeout must be greater than 0")
	}
	if err := validatePatterns(plugin.Exclude); err != nil {
		return sensu.CheckStateUnknown, err
	}
	return sensu.CheckStateOK, nil
}

func executeCheck(event *corev2.Event) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(plugin.Timeout)*time.Second)
	defer cancel()

	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return sensu.CheckStateUnknown, fmt.Errorf("connecting to systemd over D-Bus: %w", err)
	}
	defer conn.Close()

	// The state filter matches load, active and sub states alike.
	unhealthy, err := conn.ListUnitsByPatternsContext(ctx, unhealthyStates, typePatterns(plugin.Types))
	if err != nil {
		return sensu.CheckStateUnknown, fmt.Errorf("listing failed units: %w", err)
	}

	var required []dbus.UnitStatus
	if len(plugin.Units) > 0 {
		// Unlike ListUnits, ListUnitsByNames also reports units that are not loaded or do not exist.
		required, err = conn.ListUnitsByNamesContext(ctx, plugin.Units)
		if err != nil {
			return sensu.CheckStateUnknown, fmt.Errorf("querying units %v: %w", plugin.Units, err)
		}
	}

	result := evaluate(unhealthy, required, plugin.Exclude)
	fmt.Printf("%s %s: %s\n", plugin.Name, stateLabel(result.State), result.Message)
	return result.State, nil
}

// unhealthyStates are the unit states queried from systemd: "failed" is critical,
// the auto-restart sub states (restart loop) are warnings.
var unhealthyStates = []string{"failed", "auto-restart", "auto-restart-queued"}

// Result is the outcome of the evaluation of the unit statuses.
type Result struct {
	State   int
	Message string
}

type problem struct {
	unit   string
	detail string
	state  int
}

// evaluate turns the unit statuses returned by systemd into a check result.
// unhealthy holds units matching unhealthyStates, required the units given with --unit.
func evaluate(unhealthy, required []dbus.UnitStatus, exclude []string) Result {
	problems := map[string]problem{}

	for _, u := range unhealthy {
		if excluded(u.Name, exclude) {
			continue
		}
		state := sensu.CheckStateWarning
		if u.ActiveState == "failed" || u.LoadState == "error" {
			state = sensu.CheckStateCritical
		}
		problems[u.Name] = problem{u.Name, fmt.Sprintf("%s/%s", u.ActiveState, u.SubState), state}
	}

	// Required units are reported even when excluded: being listed explicitly wins.
	for _, u := range required {
		switch {
		case u.LoadState != "loaded":
			problems[u.Name] = problem{u.Name, "load " + u.LoadState, sensu.CheckStateCritical}
		case u.ActiveState != "active":
			problems[u.Name] = problem{u.Name, fmt.Sprintf("%s/%s", u.ActiveState, u.SubState), sensu.CheckStateCritical}
		}
	}

	if len(problems) == 0 {
		msg := "no failed unit"
		if len(required) > 0 {
			msg += fmt.Sprintf(", %d required unit(s) active", len(required))
		}
		return Result{sensu.CheckStateOK, msg}
	}

	names := make([]string, 0, len(problems))
	worst := sensu.CheckStateOK
	for name, p := range problems {
		names = append(names, name)
		worst = max(worst, p.state)
	}
	sort.Strings(names)

	details := make([]string, 0, len(names))
	for _, name := range names {
		details = append(details, fmt.Sprintf("%s (%s)", name, problems[name].detail))
	}
	return Result{worst, fmt.Sprintf("%d unit(s) in trouble: %s", len(names), strings.Join(details, ", "))}
}

func excluded(name string, patterns []string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}

func validatePatterns(patterns []string) error {
	for _, p := range patterns {
		if _, err := path.Match(p, ""); err != nil {
			return fmt.Errorf("invalid exclude pattern %q: %w", p, err)
		}
	}
	return nil
}

// typePatterns converts unit types ("service", ".timer") into systemd glob patterns.
// A nil result means every unit type.
func typePatterns(types []string) []string {
	var patterns []string
	for _, t := range types {
		patterns = append(patterns, "*."+strings.TrimPrefix(t, "."))
	}
	return patterns
}

func stateLabel(state int) string {
	switch state {
	case sensu.CheckStateOK:
		return "OK"
	case sensu.CheckStateWarning:
		return "WARNING"
	case sensu.CheckStateCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}
