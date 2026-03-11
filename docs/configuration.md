# Sentinel Configuration Reference

This document describes the Sentinel configuration options and how to set them
in three formats: YAML, command-line flags, and environment variables.

Overrides are applied in this order: CLI flags > environment variables > YAML file > defaults.

## Config file location

You can point the sentinel at a config file with:

- CLI: `--config` (or `-c`)
- Env: `HYPERFLEET_CONFIG`
- Default: `/etc/hyperfleet/config.yaml`
- Required for startup

## YAML options (SentinelConfig)

All fields use **snake_case** naming.

```yaml
sentinel:
  name: hyperfleet-sentinel-clusters

debug_config: false

log:
  level: "info"
  format: "text"
  output: "stdout"

clients:
  hyperfleet_api:
    base_url: "http://hyperfleet-api:8000"
    version: "v1"
    timeout: "10s"
  broker:
    topic: ""

resource_type: "clusters"
poll_interval: "5s"
max_age_not_ready: "10s"
max_age_ready: "30m"

resource_selector:
  - label: shard
    value: "1"
  - label: region
    value: us-east-1

message_data:
  id: "resource.id"
  kind: "resource.kind"
```

### Top-level fields

- `sentinel.name` (string, required): Sentinel component name/identifier.
- `debug_config` (bool, optional): Log the merged config after load. Default: `false`.

### Logging (`log`)

- `log.level` (string, optional): Log level (`debug`, `info`, `warn`, `error`). Default: `info`.
- `log.format` (string, optional): Log format (`text`, `json`). Default: `text`.
- `log.output` (string, optional): Log output destination (`stdout`, `stderr`). Default: `stdout`.

### HyperFleet API client (`clients.hyperfleet_api`)

- `base_url` (string, required): Base URL for HyperFleet API requests.
- `version` (string, optional): API version. Default: `v1`.
- `timeout` (duration string, optional): HTTP client timeout. Default: `10s`.

### Broker (`clients.broker`)

- `topic` (string, optional): Broker topic for publishing events.

Note: Broker implementation details (RabbitMQ URL, GCP project ID, etc.) are configured
separately via `broker.yaml` or the hyperfleet-broker library environment variables.

### Sentinel-specific

- `resource_type` (string, required): Resource type to watch (`clusters`, `nodepools`).
- `poll_interval` (duration string, required): How often to poll the API. Default: `5s`.
- `max_age_not_ready` (duration string, required): Max age for not-ready resources. Default: `10s`.
- `max_age_ready` (duration string, required): Max age for ready resources. Default: `30m`.
- `resource_selector` (list, optional): Label selectors to filter resources. Empty means watch all.
- `message_data` (map, required): CEL expressions defining the CloudEvent payload structure.

## Command-line parameters

The following CLI flags override YAML values:

**General**

- `--debug-config` -> `debug_config`
- `--name` -> `sentinel.name`
- `--log-level` -> `log.level`
- `--log-format` -> `log.format`
- `--log-output` -> `log.output`

**HyperFleet API**

- `--hyperfleet-api-base-url` -> `clients.hyperfleet_api.base_url`
- `--hyperfleet-api-version` -> `clients.hyperfleet_api.version`
- `--hyperfleet-api-timeout` -> `clients.hyperfleet_api.timeout`

**Broker**

- `--broker-topic` -> `clients.broker.topic`

**Sentinel**

- `--resource-type` -> `resource_type`
- `--poll-interval` -> `poll_interval`
- `--max-age-not-ready` -> `max_age_not_ready`
- `--max-age-ready` -> `max_age_ready`

## Environment variables

All deployment overrides use the `HYPERFLEET_` prefix unless noted.

**General**

- `HYPERFLEET_DEBUG_CONFIG` -> `debug_config`
- `HYPERFLEET_SENTINEL_NAME` -> `sentinel.name`
- `HYPERFLEET_LOG_LEVEL` -> `log.level`
- `HYPERFLEET_LOG_FORMAT` -> `log.format`
- `HYPERFLEET_LOG_OUTPUT` -> `log.output`

**HyperFleet API**

- `HYPERFLEET_API_BASE_URL` -> `clients.hyperfleet_api.base_url`
- `HYPERFLEET_API_VERSION` -> `clients.hyperfleet_api.version`
- `HYPERFLEET_API_TIMEOUT` -> `clients.hyperfleet_api.timeout`

**Broker**

- `HYPERFLEET_BROKER_TOPIC` -> `clients.broker.topic`

**Sentinel**

- `HYPERFLEET_RESOURCE_TYPE` -> `resource_type`
- `HYPERFLEET_POLL_INTERVAL` -> `poll_interval`
- `HYPERFLEET_MAX_AGE_NOT_READY` -> `max_age_not_ready`
- `HYPERFLEET_MAX_AGE_READY` -> `max_age_ready`

## Examples

### Override API endpoint via environment variable

```bash
export HYPERFLEET_API_BASE_URL=http://localhost:8080
./bin/sentinel serve --config=config.yaml
```

### Override log level via CLI flag

```bash
./bin/sentinel serve --config=config.yaml --log-level=debug
```

### Override multiple settings

```bash
export HYPERFLEET_API_BASE_URL=http://api-staging:8000
export HYPERFLEET_LOG_LEVEL=debug
export HYPERFLEET_LOG_FORMAT=json
./bin/sentinel serve --config=config.yaml --poll-interval=2s
```

### Precedence example

Given this config file:

```yaml
log:
  level: "info"
```

And these overrides:

```bash
export HYPERFLEET_LOG_LEVEL=warn
./bin/sentinel serve --config=config.yaml --log-level=debug
```

The final log level will be `debug` (CLI flag wins over env var and config file).
