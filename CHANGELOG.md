# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Initial release of HyperFleet Sentinel Service
- Kubernetes service that polls HyperFleet API for resource updates
- Support for clusters and nodepools resource types
- Configurable polling intervals and max age intervals (not ready vs ready resources)
- Horizontal sharding via resource selector labels
- CloudEvents publishing with broker abstraction (GCP Pub/Sub, RabbitMQ, Stub)
- CEL-based message data templating for custom CloudEvents payloads
- Prometheus metrics for observability
- Grafana dashboard for monitoring
- PodMonitoring support for GKE with Google Cloud Managed Prometheus
- ServiceMonitor support for Prometheus Operator
- Helm chart for deployment
- Integration tests with testcontainers (RabbitMQ and GCP Pub/Sub)
- OpenAPI client generation from hyperfleet-api specification
- Configuration validation at startup
- Standard configuration pattern following HyperFleet architecture

### Changed

### Deprecated

### Removed

### Fixed

### Security

---

<!-- Changelog Guidelines:

Follow these guidelines when updating the changelog:

1. **What to include:**
   - All notable changes that affect users
   - New features, bug fixes, security fixes
   - Breaking changes (mark with "BREAKING CHANGE" in description)
   - Deprecations and removals

2. **What NOT to include:**
   - Internal refactoring that doesn't affect users, i.e. editorial/layout fixes only; component boundary or interface changes MUST be logged as they impact E2E testing
   - Development tooling changes
   - Documentation typo fixes
   - Code formatting changes

3. **How to categorize changes:**
   - **Added** for new features
   - **Changed** for changes in existing functionality
   - **Deprecated** for soon-to-be removed features
   - **Removed** for now removed features
   - **Fixed** for any bug fixes
   - **Security** for vulnerability fixes

4. **Version format:**
   - Use semantic versioning (MAJOR.MINOR.PATCH)
   - Include release date in YYYY-MM-DD format
   - Link to release tags if available

5. **Entry format:**
   ```markdown
   ### Added
   - Brief description of the change
   - Another change with [link to issue/PR](URL) if relevant
   ```

6. **Example entries:**
   ```markdown
   ### Added
   - New API endpoint for cluster status monitoring
   - Support for custom authentication providers

   ### Changed
   - BREAKING CHANGE: Updated API response format for cluster resources
   - Improved error handling for network timeouts

   ### Fixed
   - Fixed memory leak in status polling service
   - Resolved authentication timeout issues
   ```
-->
