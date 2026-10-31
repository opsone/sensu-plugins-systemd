# check-systemd

Sensu Go check reporting failed systemd units, including the ones failing silently (e.g. `certbot.service` triggered by a timer, `fail2ban.service` gone after a reload).

Queries systemd over D-Bus (no `systemctl` output parsing, no CGO, static binary).

## Behavior

| Situation | State |
|---|---|
| Unit in `failed` state (all types by default: service, timer, mount, socket…) | CRITICAL |
| Unit stuck in a restart loop (`auto-restart`) | WARNING |
| Unit given with `--unit` missing or not `active` | CRITICAL |
| D-Bus error / invalid argument | UNKNOWN |

A unit given with `--unit` is checked even if it matches an `--exclude` pattern.

## Usage

```
check-systemd [flags]

  -u, --unit strings      Unit that must be active (repeatable)
  -e, --exclude strings   Glob pattern of units to ignore (repeatable)
  -t, --type strings      Unit types inspected for failures (repeatable, default: all)
      --timeout int       D-Bus timeout in seconds (default 10)
```

Every option can also be set with an environment variable (`CHECK_SYSTEMD_UNITS`, `CHECK_SYSTEMD_EXCLUDE`, `CHECK_SYSTEMD_TYPES`, `CHECK_SYSTEMD_TIMEOUT`) or an entity annotation `sensu.io/plugins/check-systemd/config/<option>`.

Examples:

```sh
# Any failed unit on the host
check-systemd

# Also: nginx and fail2ban must be running, the certbot timer must be armed
check-systemd -u nginx.service -u fail2ban.service -u certbot.timer

# Ignore user sessions
check-systemd -e 'user@*.service' -e 'session-*.scope'
```

For a `oneshot` service such as `certbot.service`, monitor its **timer** with `--unit`: the service is `inactive` between runs, and its failure is detected anyway through the `failed` state.

Note: a failed unit stays `failed` until its next successful run or a `systemctl reset-failed <unit>`.

## Check definition

```yaml
---
type: CheckConfig
api_version: core/v2
metadata:
  name: check-systemd
spec:
  command: check-systemd -e 'user@*.service'
  runtime_assets:
    - opsone/sensu-plugins-systemd
  interval: 300
  publish: true
  subscriptions:
    - linux
```

The Sensu agent does not need to run as root: reading unit states over the system bus is allowed to unprivileged users.

## Build and release

```sh
make test          # go vet + unit tests
make integration   # runs the check against a real systemd in Docker
```

Release:

```sh
git tag 0.1.0 && git push origin 0.1.0
make release       # version taken from the tag, or forced: make release VERSION=0.1.0
sensuctl create -f dist/opsone-sensu-plugins-systemd_0.1.0.yml
```

`make release` runs `make dist`, which runs the tests then produces in `dist/`:

- one archive per architecture (`linux/amd64`, `linux/arm64`) in Sensu asset layout (`bin/check-systemd`);
- the sha512 checksums file;
- `opsone-sensu-plugins-systemd_<version>.yml`, the asset definition pointing to the GitHub release of the tag.

It then creates the GitHub release of the tag with `gh`, uploads the archives and checksums, and uses the asset definition as release description.

`check-systemd version` prints the version, commit and build date.
