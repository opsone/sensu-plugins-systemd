#!/usr/bin/env bash
# Runs check-systemd against a real systemd inside a Docker container.
set -euo pipefail
cd "$(dirname "$0")"

image=check-systemd-integration
container=check-systemd-integration-$$

docker build -q -t "$image" . >/dev/null
docker run -d --rm --privileged --tmpfs /run --tmpfs /run/lock --name "$container" "$image" >/dev/null
trap 'docker stop "$container" >/dev/null' EXIT

docker exec "$container" systemctl is-system-running --wait >/dev/null || true

# Wait for the test units to reach their broken states before checking.
for _ in $(seq 30); do
  docker exec "$container" systemctl start --no-block broken.service flappy.service >/dev/null 2>&1 || true
  if [[ $(docker exec "$container" systemctl show -P ActiveState broken.service) == failed &&
        $(docker exec "$container" systemctl show -P SubState flappy.service) == auto-restart ]]; then
    break
  fi
  sleep 1
done

# Run as an unprivileged user, like sensu-agent does.
others=$(docker exec "$container" systemctl list-units --state=failed --plain --no-legend \
  | awk '$1 != "broken.service" {printf " -e %s", $1}')

failures=0
expect() {
  local want=$1; shift
  local out rc=0
  out=$(docker exec "$container" runuser -u nobody -- check-systemd "$@") || rc=$?
  if [[ $rc -eq $want ]]; then
    echo "ok   [$rc] check-systemd $* -> $out"
  else
    echo "FAIL [$rc, want $want] check-systemd $* -> $out"
    failures=$((failures + 1))
  fi
}

expect 2 $others
expect 1 $others -e broken.service
expect 0 $others -e broken.service -e flappy.service
expect 0 $others -e broken.service -e flappy.service -u dbus.service
expect 2 $others -e broken.service -e flappy.service -u nginx.service
expect 2 $others -e '*' -u broken.service

exit $failures
