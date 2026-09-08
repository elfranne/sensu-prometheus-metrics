# Changelog
All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/en/1.0.0/)
and this project adheres to [Semantic
Versioning](http://semver.org/spec/v2.0.0.html).

## Unreleased

## [0.3.0] - 2026-09-08

### Fixed
- Scraping panicked with `Invalid name validation scheme requested: unset`
  instead of returning metrics. prometheus/common v0.71.0 gave
  `expfmt.TextParser` a name validation scheme whose zero value is invalid, so
  the parser is now built with `expfmt.NewTextParser` and UTF-8 name
  validation.

### Added
- Test suite covering the exporter scrape: parsing, added labels, millisecond
  timestamps, basic auth, TLS and mTLS, and the UNKNOWN path taken when the
  exporter cannot be scraped.

### Changed
- Updated Go to 1.26.5, prometheus/common to v0.71.0, sensu/core/v2 to v2.21.5,
  and the GitHub Actions to actions/checkout v7, actions/setup-go v7,
  golangci-lint-action v9 and goreleaser-action v7.
- Every workflow now declares the `permissions` it needs, the golangci-lint
  version is pinned, and release.yml checks out the full history with
  `fetch-depth` instead of a separate unshallow step.
- Updated the GoReleaser config to the version 2 schema: declared `version: 2`,
  replaced the deprecated `archives.format` with `formats`, and dropped the
  `goos`/`goarch`/`goarm` lists that are ignored when `targets` is set. Archive
  names are unchanged, so the Bonsai asset definition still matches.
- Rewrote the README with the plugin's own documentation in place of the check
  plugin template text.

### Removed
- The Tag workflow.

## [0.2.2] - 2025-01-11

### Changed
- Updated the Go version and the modules.

## [0.2.1] - 2024-10-10

### Changed
- Updated modules.

## [0.2] - 2024-09-16

### Changed
- Updated Go and libraries.

## [0.1.2] - 2024-07-02

### Changed
- Metrics are timestamped with Unix milliseconds, matching what Prometheus and
  Sensu's `prometheus_text` format expect.

## [0.1.1] - 2024-07-01

### Added
- Basic auth (`--user`, `--password`) and mTLS (`--cert`, `--key`, `--cacert`,
  `--insecureskipverify`) for the exporter connection.

## [0.1.0] - 2024-07-01

### Added
- Initial release: scrape a Prometheus exporter (`--url`) and print the samples
  in Sensu's `prometheus_text` format, with optional extra labels (`--label`).
