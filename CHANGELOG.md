# Changelog

## [Unreleased]

### Added
- CRITICAL alert on any systemd unit in `failed` state, WARNING on a unit stuck in a restart loop (`auto-restart`).
- `--unit`: units that must be active, `--exclude`: units to ignore (glob), `--type`: filter by unit type.
