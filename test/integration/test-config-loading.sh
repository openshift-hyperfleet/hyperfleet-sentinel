#!/usr/bin/env bash
# test-config-loading.sh - Verifies that every sentinel config parameter loads correctly from
# all available sources: config file, environment variable, and CLI flag.
#
# Usage:
#   ./test/integration/test-config-loading.sh [--verbose]
#
# Output: one PASS/FAIL line per test, plus a summary at the end.
# Exit code: 0 if all tests pass, 1 if any fail.

set -euo pipefail

VERBOSE=0
for arg in "$@"; do
  [[ "$arg" == "--verbose" || "$arg" == "-v" ]] && VERBOSE=1
done

# ─── Colours ──────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'

PASS=0; FAIL=0; declare -a ERRORS=()

pass() { echo -e "  ${GREEN}PASS${NC}  $1"; PASS=$((PASS+1)); }
fail() {
  local name="$1" pattern="$2" output="$3"
  echo -e "  ${RED}FAIL${NC}  $name"
  echo "        expected pattern: ${pattern}"
  FAIL=$((FAIL+1)); ERRORS+=("$name")
  if [[ $VERBOSE -eq 1 ]]; then
    echo "        output:"
    echo "$output" | sed 's/^/          /'
  fi
}

section() { echo -e "\n${CYAN}══ $1 ══${NC}"; }

# assert_contains <test-name> <output> <fixed-string-pattern>
assert_contains() {
  local name="$1" output="$2" pattern="$3"
  if echo "$output" | grep -qF "$pattern"; then
    pass "$name"
  else
    fail "$name" "$pattern" "$output"
  fi
}

# assert_not_contains <test-name> <output> <fixed-string-pattern>
assert_not_contains() {
  local name="$1" output="$2" pattern="$3"
  if echo "$output" | grep -qF "$pattern"; then
    fail "$name" "NOT: $pattern" "$output"
  else
    pass "$name"
  fi
}

# ─── Setup ────────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
TMPDIR_TEST="$(mktemp -d)"

# If a pre-built binary is present at ROOT_DIR/bin/sentinel (placed by the test harness),
# use it directly; otherwise build from source using the local Go toolchain.
if [[ -x "$ROOT_DIR/bin/sentinel" ]]; then
  echo -e "${YELLOW}Using pre-built sentinel binary...${NC}"
  SENTINEL_BIN="$ROOT_DIR/bin/sentinel"
  _SENTINEL_OWN=0
else
  SENTINEL_BIN="$(mktemp /tmp/sentinel-test-XXXXXX)"
  _SENTINEL_OWN=1
  echo -e "${YELLOW}Building sentinel binary...${NC}"
  (cd "$ROOT_DIR" && go build -o "$SENTINEL_BIN" ./cmd/sentinel)
  echo "  Built: $SENTINEL_BIN"
fi

cleanup() { [[ ${_SENTINEL_OWN:-0} -eq 1 ]] && rm -f "$SENTINEL_BIN"; rm -rf "$TMPDIR_TEST"; }
trap cleanup EXIT

# ─── Config-dump wrapper ───────────────────────────────────────────────────────
# cfg_dump <config_file> [extra CLI flags...]
# Caller must set env vars in the calling environment (use subshells).
cfg_dump() {
  local config="$1"; shift
  "$SENTINEL_BIN" config-dump -c "$config" "$@" 2>/dev/null
}

cfg_dump_no_flag() {
  "$SENTINEL_BIN" config-dump "$@" 2>/dev/null
}

# ─── Config file factory ──────────────────────────────────────────────────────
# sentinel_config <file> [extra yaml lines...]
#
# Creates a minimal sentinel config. The file ends with "clients:" so that:
#   - 2-space-indented extra args become children of clients
#   - 0-space-indented extra args become root-level keys (e.g. log:, debug_config:, poll_interval:)
#
# Required fields NOT included (must be passed as extra args):
#   poll_interval, max_age_not_ready, max_age_ready, clients.hyperfleet_api.*
sentinel_config() {
  local file="$1"; shift
  {
    cat <<'YAML'
sentinel:
  name: test-sentinel
resource_type: clusters
message_data:
  id: "resource.id"
  kind: "resource.kind"
clients:
YAML
    printf '%s\n' "$@"
  } >"$file"
}

CFG="$TMPDIR_TEST/sentinel.yaml"  # reused across tests (overwritten each time)

# ─── Base config fragments ─────────────────────────────────────────────────────
# Combine these as extra args to sentinel_config to satisfy validation.
# Indentation determines YAML placement:
#   BASE_API, BASE_BROKER  → children of clients: (2-space indent)
#   BASE_TIMING            → root-level keys      (0-space indent)
BASE_API=(
  "  hyperfleet_api:"
  "    base_url: https://base.example.com"
  "    timeout: 10s"
)
BASE_BROKER=(
  "  broker:"
  "    topic: base-topic"
)
BASE_TIMING=(
  "poll_interval: 5s"
  "max_age_not_ready: 10s"
  "max_age_ready: 30m"
)

# ─────────────────────────────────────────────────────────────────────────────
section "Sentinel identity"
# ─────────────────────────────────────────────────────────────────────────────

sentinel_config "$CFG" "${BASE_API[@]}" "${BASE_TIMING[@]}"
out=$(cfg_dump "$CFG")
assert_contains "sentinel.name [file]" "$out" "name: test-sentinel"

assert_contains "sentinel.name [env]"     "$(HYPERFLEET_SENTINEL_NAME=env-name cfg_dump "$CFG")"                                          "name: env-name"
assert_contains "sentinel.name [cli]"     "$(cfg_dump "$CFG" --sentinel-name=cli-name)"                                                   "name: cli-name"
assert_contains "sentinel.name [cli>env]" "$(HYPERFLEET_SENTINEL_NAME=env-name cfg_dump "$CFG" --sentinel-name=cli-name)"                 "name: cli-name"

# ─────────────────────────────────────────────────────────────────────────────
section "HyperFleet API"
# ─────────────────────────────────────────────────────────────────────────────

# base_url
sentinel_config "$CFG" "  hyperfleet_api:" "    base_url: https://file-api.example.com" "    timeout: 10s" "${BASE_TIMING[@]}"
assert_contains "api.base_url [file]"    "$(cfg_dump "$CFG")"                                                                                                              "base_url: https://file-api.example.com"
assert_contains "api.base_url [env]"     "$(HYPERFLEET_API_BASE_URL=https://env-api.example.com cfg_dump "$CFG")"                                                          "base_url: https://env-api.example.com"
assert_contains "api.base_url [cli]"     "$(cfg_dump "$CFG" --hyperfleet-api-base-url=https://cli-api.example.com)"                                                       "base_url: https://cli-api.example.com"
assert_contains "api.base_url [cli>env]" "$(HYPERFLEET_API_BASE_URL=https://env-api.example.com cfg_dump "$CFG" --hyperfleet-api-base-url=https://cli-api.example.com)"   "base_url: https://cli-api.example.com"

# version
sentinel_config "$CFG" "  hyperfleet_api:" "    base_url: https://base.example.com" "    timeout: 10s" "    version: file-v99" "${BASE_TIMING[@]}"
assert_contains "api.version [file]"    "$(cfg_dump "$CFG")"                                                                  "version: file-v99"
assert_contains "api.version [env]"     "$(HYPERFLEET_API_VERSION=env-v88 cfg_dump "$CFG")"                                   "version: env-v88"
assert_contains "api.version [cli]"     "$(cfg_dump "$CFG" --hyperfleet-api-version=cli-v77)"                                 "version: cli-v77"
assert_contains "api.version [cli>env]" "$(HYPERFLEET_API_VERSION=env-v88 cfg_dump "$CFG" --hyperfleet-api-version=cli-v77)"  "version: cli-v77"

# timeout
sentinel_config "$CFG" "  hyperfleet_api:" "    base_url: https://base.example.com" "    timeout: 11s" "${BASE_TIMING[@]}"
assert_contains "api.timeout [file]"    "$(cfg_dump "$CFG")"                                                              "timeout: 11s"
assert_contains "api.timeout [env]"     "$(HYPERFLEET_API_TIMEOUT=22s cfg_dump "$CFG")"                                   "timeout: 22s"
assert_contains "api.timeout [cli]"     "$(cfg_dump "$CFG" --hyperfleet-api-timeout=33s)"                                 "timeout: 33s"
assert_contains "api.timeout [cli>env]" "$(HYPERFLEET_API_TIMEOUT=22s cfg_dump "$CFG" --hyperfleet-api-timeout=33s)"     "timeout: 33s"

# retry_attempts
sentinel_config "$CFG" "  hyperfleet_api:" "    base_url: https://base.example.com" "    timeout: 10s" "    retry_attempts: 11" "${BASE_TIMING[@]}"
assert_contains "api.retry_attempts [file]"    "$(cfg_dump "$CFG")"                                                                         "retry_attempts: 11"
assert_contains "api.retry_attempts [env]"     "$(HYPERFLEET_API_RETRY_ATTEMPTS=22 cfg_dump "$CFG")"                                        "retry_attempts: 22"
assert_contains "api.retry_attempts [cli]"     "$(cfg_dump "$CFG" --hyperfleet-api-retry-attempts=33)"                                      "retry_attempts: 33"
assert_contains "api.retry_attempts [cli>env]" "$(HYPERFLEET_API_RETRY_ATTEMPTS=22 cfg_dump "$CFG" --hyperfleet-api-retry-attempts=33)"     "retry_attempts: 33"

# retry_backoff
sentinel_config "$CFG" "  hyperfleet_api:" "    base_url: https://base.example.com" "    timeout: 10s" "    retry_backoff: linear" "${BASE_TIMING[@]}"
assert_contains "api.retry_backoff [file]"    "$(cfg_dump "$CFG")"                                                                                  "retry_backoff: linear"
assert_contains "api.retry_backoff [env]"     "$(HYPERFLEET_API_RETRY_BACKOFF=constant cfg_dump "$CFG")"                                            "retry_backoff: constant"
assert_contains "api.retry_backoff [cli]"     "$(cfg_dump "$CFG" --hyperfleet-api-retry-backoff=exponential)"                                       "retry_backoff: exponential"
assert_contains "api.retry_backoff [cli>env]" "$(HYPERFLEET_API_RETRY_BACKOFF=constant cfg_dump "$CFG" --hyperfleet-api-retry-backoff=exponential)" "retry_backoff: exponential"

# base_delay
sentinel_config "$CFG" "  hyperfleet_api:" "    base_url: https://base.example.com" "    timeout: 10s" "    base_delay: 11s" "${BASE_TIMING[@]}"
assert_contains "api.base_delay [file]"    "$(cfg_dump "$CFG")"                                                                "base_delay: 11s"
assert_contains "api.base_delay [env]"     "$(HYPERFLEET_API_BASE_DELAY=22s cfg_dump "$CFG")"                                  "base_delay: 22s"
assert_contains "api.base_delay [cli]"     "$(cfg_dump "$CFG" --hyperfleet-api-base-delay=33s)"                                "base_delay: 33s"
assert_contains "api.base_delay [cli>env]" "$(HYPERFLEET_API_BASE_DELAY=22s cfg_dump "$CFG" --hyperfleet-api-base-delay=33s)"  "base_delay: 33s"

# max_delay — use sub-60s values since time.Duration.String() reformats e.g. 111s → 1m51s
sentinel_config "$CFG" "  hyperfleet_api:" "    base_url: https://base.example.com" "    timeout: 10s" "    max_delay: 51s" "${BASE_TIMING[@]}"
assert_contains "api.max_delay [file]"    "$(cfg_dump "$CFG")"                                                               "max_delay: 51s"
assert_contains "api.max_delay [env]"     "$(HYPERFLEET_API_MAX_DELAY=52s cfg_dump "$CFG")"                                  "max_delay: 52s"
assert_contains "api.max_delay [cli]"     "$(cfg_dump "$CFG" --hyperfleet-api-max-delay=53s)"                                "max_delay: 53s"
assert_contains "api.max_delay [cli>env]" "$(HYPERFLEET_API_MAX_DELAY=52s cfg_dump "$CFG" --hyperfleet-api-max-delay=53s)"   "max_delay: 53s"

# ─────────────────────────────────────────────────────────────────────────────
section "Broker"
# ─────────────────────────────────────────────────────────────────────────────

sentinel_config "$CFG" "${BASE_API[@]}" "  broker:" "    topic: file-topic" "${BASE_TIMING[@]}"

# topic — standard env var
assert_contains "broker.topic [file]"                        "$(cfg_dump "$CFG")"                                                                                                  "topic: file-topic"
assert_contains "broker.topic [env]"                         "$(HYPERFLEET_BROKER_TOPIC=env-topic cfg_dump "$CFG")"                                                                "topic: env-topic"
assert_contains "broker.topic [cli]"                         "$(cfg_dump "$CFG" --broker-topic=cli-topic)"                                                                         "topic: cli-topic"
assert_contains "broker.topic [cli>env]"                     "$(HYPERFLEET_BROKER_TOPIC=env-topic cfg_dump "$CFG" --broker-topic=cli-topic)"                                       "topic: cli-topic"

# ─────────────────────────────────────────────────────────────────────────────
section "Log"
# ─────────────────────────────────────────────────────────────────────────────

# level
sentinel_config "$CFG" "${BASE_API[@]}" "${BASE_TIMING[@]}" "log:" "  level: debug"
assert_contains "log.level [file]"     "$(cfg_dump "$CFG")"                                                           "level: debug"
assert_contains "log.level [env]"      "$(HYPERFLEET_LOG_LEVEL=warn cfg_dump "$CFG")"                                 "level: warn"
assert_contains "log.level [cli]"      "$(cfg_dump "$CFG" --log-level=error)"                                         "level: error"
assert_contains "log.level [cli>env]"  "$(HYPERFLEET_LOG_LEVEL=warn cfg_dump "$CFG" --log-level=error)"               "level: error"
assert_contains "log.level [env>file]" "$(HYPERFLEET_LOG_LEVEL=warn cfg_dump "$CFG")"                                 "level: warn"

# format
sentinel_config "$CFG" "${BASE_API[@]}" "${BASE_TIMING[@]}" "log:" "  format: json"
assert_contains "log.format [file]"    "$(cfg_dump "$CFG")"                                                           "format: json"
assert_contains "log.format [env]"     "$(HYPERFLEET_LOG_FORMAT=text cfg_dump "$CFG")"                                "format: text"
assert_contains "log.format [cli]"     "$(cfg_dump "$CFG" --log-format=json)"                                         "format: json"
assert_contains "log.format [cli>env]" "$(HYPERFLEET_LOG_FORMAT=text cfg_dump "$CFG" --log-format=json)"              "format: json"

# output
sentinel_config "$CFG" "${BASE_API[@]}" "${BASE_TIMING[@]}" "log:" "  output: stderr"
assert_contains "log.output [file]"    "$(cfg_dump "$CFG")"                                                           "output: stderr"
assert_contains "log.output [env]"     "$(HYPERFLEET_LOG_OUTPUT=stdout cfg_dump "$CFG")"                              "output: stdout"
assert_contains "log.output [cli]"     "$(cfg_dump "$CFG" --log-output=stderr)"                                       "output: stderr"
assert_contains "log.output [cli>env]" "$(HYPERFLEET_LOG_OUTPUT=stdout cfg_dump "$CFG" --log-output=stderr)"          "output: stderr"

# ─────────────────────────────────────────────────────────────────────────────
section "Sentinel-specific parameters"
# ─────────────────────────────────────────────────────────────────────────────

# resource_type
sentinel_config "$CFG" "${BASE_API[@]}" "${BASE_TIMING[@]}"
assert_contains "resource_type [file]"    "$(cfg_dump "$CFG")"                                                                "resource_type: clusters"
assert_contains "resource_type [env]"     "$(HYPERFLEET_RESOURCE_TYPE=nodepools cfg_dump "$CFG")"                            "resource_type: nodepools"
assert_contains "resource_type [cli]"     "$(cfg_dump "$CFG" --resource-type=nodepools)"                                     "resource_type: nodepools"
assert_contains "resource_type [cli>env]" "$(HYPERFLEET_RESOURCE_TYPE=nodepools cfg_dump "$CFG" --resource-type=clusters)"   "resource_type: clusters"

# poll_interval
sentinel_config "$CFG" "${BASE_API[@]}" "poll_interval: 11s" "max_age_not_ready: 10s" "max_age_ready: 30m"
assert_contains "poll_interval [file]"    "$(cfg_dump "$CFG")"                                                          "poll_interval: 11s"
assert_contains "poll_interval [env]"     "$(HYPERFLEET_POLL_INTERVAL=22s cfg_dump "$CFG")"                             "poll_interval: 22s"
assert_contains "poll_interval [cli]"     "$(cfg_dump "$CFG" --poll-interval=33s)"                                      "poll_interval: 33s"
assert_contains "poll_interval [cli>env]" "$(HYPERFLEET_POLL_INTERVAL=22s cfg_dump "$CFG" --poll-interval=33s)"         "poll_interval: 33s"

# max_age_not_ready
sentinel_config "$CFG" "${BASE_API[@]}" "poll_interval: 5s" "max_age_not_ready: 11s" "max_age_ready: 30m"
assert_contains "max_age_not_ready [file]"    "$(cfg_dump "$CFG")"                                                              "max_age_not_ready: 11s"
assert_contains "max_age_not_ready [env]"     "$(HYPERFLEET_MAX_AGE_NOT_READY=22s cfg_dump "$CFG")"                            "max_age_not_ready: 22s"
assert_contains "max_age_not_ready [cli]"     "$(cfg_dump "$CFG" --max-age-not-ready=33s)"                                     "max_age_not_ready: 33s"
assert_contains "max_age_not_ready [cli>env]" "$(HYPERFLEET_MAX_AGE_NOT_READY=22s cfg_dump "$CFG" --max-age-not-ready=33s)"    "max_age_not_ready: 33s"

# max_age_ready — use sub-60s values to avoid duration reformatting (e.g. 111s → 1m51s)
sentinel_config "$CFG" "${BASE_API[@]}" "poll_interval: 5s" "max_age_not_ready: 10s" "max_age_ready: 51s"
assert_contains "max_age_ready [file]"    "$(cfg_dump "$CFG")"                                                          "max_age_ready: 51s"
assert_contains "max_age_ready [env]"     "$(HYPERFLEET_MAX_AGE_READY=52s cfg_dump "$CFG")"                             "max_age_ready: 52s"
assert_contains "max_age_ready [cli]"     "$(cfg_dump "$CFG" --max-age-ready=53s)"                                      "max_age_ready: 53s"
assert_contains "max_age_ready [cli>env]" "$(HYPERFLEET_MAX_AGE_READY=52s cfg_dump "$CFG" --max-age-ready=53s)"         "max_age_ready: 53s"

# ─────────────────────────────────────────────────────────────────────────────
section "debug_config flag"
# ─────────────────────────────────────────────────────────────────────────────

sentinel_config "$CFG" "${BASE_API[@]}" "${BASE_TIMING[@]}" "debug_config: true"
assert_contains     "debug_config [file=true]"    "$(cfg_dump "$CFG")"                                     "debug_config: true"

sentinel_config "$CFG" "${BASE_API[@]}" "${BASE_TIMING[@]}"
assert_not_contains "debug_config [default=false]" "$(cfg_dump "$CFG")"                                    "debug_config: true"
assert_contains     "debug_config [env=true]"      "$(HYPERFLEET_DEBUG_CONFIG=true cfg_dump "$CFG")"       "debug_config: true"
assert_contains     "debug_config [cli=true]"      "$(cfg_dump "$CFG" --debug-config)"                     "debug_config: true"

# ─────────────────────────────────────────────────────────────────────────────
section "Priority verification (cross-parameter)"
# ─────────────────────────────────────────────────────────────────────────────
# Use api.base_url as the representative parameter for all priority checks.

sentinel_config "$CFG" "  hyperfleet_api:" "    base_url: https://file.example.com" "    timeout: 10s" "${BASE_TIMING[@]}"

assert_contains "priority: file only → file value"    "$(cfg_dump "$CFG")"                                                                                                      "base_url: https://file.example.com"
assert_contains "priority: env > file"                "$(HYPERFLEET_API_BASE_URL=https://env.example.com cfg_dump "$CFG")"                                                       "base_url: https://env.example.com"
assert_contains "priority: cli > file"                "$(cfg_dump "$CFG" --hyperfleet-api-base-url=https://cli.example.com)"                                                     "base_url: https://cli.example.com"
assert_contains "priority: cli > env"                 "$(HYPERFLEET_API_BASE_URL=https://env.example.com cfg_dump "$CFG" --hyperfleet-api-base-url=https://cli.example.com)"    "base_url: https://cli.example.com"
assert_contains "priority: env does not override cli" "$(HYPERFLEET_API_BASE_URL=https://env.example.com cfg_dump "$CFG" --hyperfleet-api-base-url=https://cli.example.com)"    "base_url: https://cli.example.com"

# ─────────────────────────────────────────────────────────────────────────────
section "Config file resolution"
# ─────────────────────────────────────────────────────────────────────────────

sentinel_config "$CFG" "${BASE_API[@]}" "${BASE_TIMING[@]}"
assert_contains "HYPERFLEET_CONFIG [selects file]" \
  "$(HYPERFLEET_CONFIG="$CFG" cfg_dump_no_flag)" \
  "name: test-sentinel"

# ─────────────────────────────────────────────────────────────────────────────
# Summary
# ─────────────────────────────────────────────────────────────────────────────
echo ""
echo "─────────────────────────────────────────"
TOTAL=$((PASS+FAIL))
if [[ $FAIL -eq 0 ]]; then
  echo -e "${GREEN}All $TOTAL tests passed.${NC}"
else
  echo -e "${RED}$FAIL/$TOTAL tests FAILED:${NC}"
  for e in "${ERRORS[@]}"; do
    echo "  - $e"
  done
fi
echo ""
[[ $FAIL -eq 0 ]]
