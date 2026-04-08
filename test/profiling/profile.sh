#!/bin/bash
set -euo pipefail
#
# Profiles Sentinel CPU and memory usage using kubectl top.
# Outputs a timestamped CSV file for analysis with analyze.py.
#
# Usage:
#   CLUSTER_COUNT=1000 DURATION_MIN=10 ./profile.sh

CLUSTER_COUNT="${CLUSTER_COUNT:-100}"
DURATION_MIN="${DURATION_MIN:-5}"

# Kubernetes target
NAMESPACE="hyperfleet"
POD_LABEL="app.kubernetes.io/instance=hyperfleet-sentinel-clusters"

# Validate dependencies
if ! command -v kubectl &>/dev/null; then
    echo "Error: kubectl not found in PATH" >&2
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OUTPUT_DIR="${SCRIPT_DIR}/results"
mkdir -p "$OUTPUT_DIR" || { echo "Error: Cannot create output directory $OUTPUT_DIR"; exit 1; }
OUTPUT_FILE="${OUTPUT_DIR}/profile-${CLUSTER_COUNT}-clusters-$(date '+%Y%m%d-%H%M%S').csv"

echo "Profiling sentinel with $CLUSTER_COUNT clusters for $DURATION_MIN minutes"
echo "Results saving to: $OUTPUT_FILE"
echo ""
read -r -p "Press Enter to start or Ctrl+C to cancel"

# Wait until metrics-server has data for the pod (takes ~30-60s after pod start)
MAX_WAIT_SECONDS=120
WAIT_START=$SECONDS
echo "Waiting for metrics to become available (timeout ${MAX_WAIT_SECONDS}s)..."
until kubectl top pod -n "$NAMESPACE" -l "$POD_LABEL" --no-headers 2>/dev/null | grep -q .
do
    if [ $(( SECONDS - WAIT_START )) -ge "$MAX_WAIT_SECONDS" ]; then
        echo "Error: Timed out after ${MAX_WAIT_SECONDS}s waiting for metrics."
        echo "Check that NAMESPACE ($NAMESPACE), POD_LABEL ($POD_LABEL), and metrics-server are correct."
        exit 1
    fi
    sleep 5
done
echo "Metrics available, starting profiling"

# Calculate when to stop recording
END_TIME=$((SECONDS + DURATION_MIN * 60))

# Write CSV header
echo "TIME,POD,CPU,MEMORY" | tee "$OUTPUT_FILE"

# Record loop — samples kubectl top and writes CSV rows until duration expires
while [ "$SECONDS" -lt "$END_TIME" ]; do
    if TOP_OUTPUT=$(kubectl top pod -n "$NAMESPACE" -l "$POD_LABEL" --no-headers 2>/dev/null) && [ -n "$TOP_OUTPUT" ]; then
        echo "$TOP_OUTPUT" | awk -v time="$(date '+%H:%M:%S')" '{print time","$1","$2","$3}' | tee -a "$OUTPUT_FILE"
    else
        echo "Warning: kubectl top failed or returned empty at $(date '+%H:%M:%S'), skipping sample" >&2
    fi
    sleep 60 # metrics-server default resolution is 60s
done

echo "Done. Results in $OUTPUT_FILE"
