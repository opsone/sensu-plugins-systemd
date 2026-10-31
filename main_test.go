package main

import (
	"reflect"
	"testing"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/sensu/sensu-plugin-sdk/sensu"
)

func unit(name, load, active, sub string) dbus.UnitStatus {
	return dbus.UnitStatus{Name: name, LoadState: load, ActiveState: active, SubState: sub}
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name      string
		unhealthy []dbus.UnitStatus
		required  []dbus.UnitStatus
		exclude   []string
		want      Result
	}{
		{
			name: "nothing to report",
			want: Result{sensu.CheckStateOK, "no failed unit"},
		},
		{
			name: "failed units are critical and sorted",
			unhealthy: []dbus.UnitStatus{
				unit("fail2ban.service", "loaded", "failed", "failed"),
				unit("certbot.service", "loaded", "failed", "failed"),
			},
			want: Result{sensu.CheckStateCritical, "2 unit(s) in trouble: certbot.service (failed/failed), fail2ban.service (failed/failed)"},
		},
		{
			name:      "restart loop is a warning",
			unhealthy: []dbus.UnitStatus{unit("php-fpm.service", "loaded", "activating", "auto-restart")},
			want:      Result{sensu.CheckStateWarning, "1 unit(s) in trouble: php-fpm.service (activating/auto-restart)"},
		},
		{
			name: "excluded units are ignored",
			unhealthy: []dbus.UnitStatus{
				unit("user@1000.service", "loaded", "failed", "failed"),
				unit("systemd-networkd-wait-online.service", "loaded", "failed", "failed"),
			},
			exclude: []string{"user@*.service", "systemd-networkd-wait-online.service"},
			want:    Result{sensu.CheckStateOK, "no failed unit"},
		},
		{
			name: "required units active",
			required: []dbus.UnitStatus{
				unit("nginx.service", "loaded", "active", "running"),
				unit("certbot.timer", "loaded", "active", "waiting"),
			},
			want: Result{sensu.CheckStateOK, "no failed unit, 2 required unit(s) active"},
		},
		{
			name: "required units stopped or missing",
			required: []dbus.UnitStatus{
				unit("nginx.service", "loaded", "inactive", "dead"),
				unit("fail2ban.service", "not-found", "inactive", "dead"),
			},
			want: Result{sensu.CheckStateCritical, "2 unit(s) in trouble: fail2ban.service (load not-found), nginx.service (inactive/dead)"},
		},
		{
			name:      "required unit wins over exclusion and is not reported twice",
			unhealthy: []dbus.UnitStatus{unit("fail2ban.service", "loaded", "failed", "failed")},
			required:  []dbus.UnitStatus{unit("fail2ban.service", "loaded", "failed", "failed")},
			exclude:   []string{"*"},
			want:      Result{sensu.CheckStateCritical, "1 unit(s) in trouble: fail2ban.service (failed/failed)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := evaluate(tt.unhealthy, tt.required, tt.exclude); got != tt.want {
				t.Errorf("evaluate() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestTypePatterns(t *testing.T) {
	if got := typePatterns(nil); got != nil {
		t.Errorf("typePatterns(nil) = %v, want nil", got)
	}
	want := []string{"*.service", "*.timer"}
	if got := typePatterns([]string{"service", ".timer"}); !reflect.DeepEqual(got, want) {
		t.Errorf("typePatterns() = %v, want %v", got, want)
	}
}

func TestValidatePatterns(t *testing.T) {
	if err := validatePatterns([]string{"user@*.service", "foo?.timer"}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := validatePatterns([]string{"[broken"}); err == nil {
		t.Error("expected an error for an invalid pattern")
	}
}
