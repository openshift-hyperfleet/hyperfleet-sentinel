# Sentinel Profiling Results

[HYPERFLEET-556](https://issues.redhat.com/browse/HYPERFLEET-556) — Validate resource limits and requests against actual Sentinel consumption.

## Goal

Validate that current resource defaults are appropriately sized under realistic load. Current defaults: CPU 100m request / 500m limit, memory 128Mi request / 512Mi limit. Ensure limits prevent noisy-neighbor issues without causing OOM kills or CPU throttling.

## Setup

- **Poll interval:** 5s (chart default)
- **Duration:** 5 min per scenario
- **Sample interval:** 60s (`kubectl top`)
- **Tracing:** Disabled
- **Mock API:** In-cluster mock-hyperfleet-api
- **Cluster counts:** 100, 1000, 5000 (number of cluster resources returned by the API per poll)
- **Tooling:** [`test/profiling/`](../test/profiling/) — `profile.sh` to capture samples, `analyze.py` to aggregate

## Results

| Clusters | CPU                | Memory               |
| -------- | ------------------ | -------------------- |
| 100      | 36–54m (avg 41m)   | 11–12Mi (avg 12Mi)   |
| 1000     | 74–88m (avg 81m)   | 26–29Mi (avg 27Mi)   |
| 5000     | 71–154m (avg 98m)  | 85–163Mi (avg 128Mi) |

## Findings

- CPU stays well under the current 500m limit at all scales. Peak was 154m at 5000 clusters. No risk of CPU throttling.
- Memory grows with cluster count. The 5000-cluster run oscillates between ~85Mi and ~163Mi. Peak 163Mi is within the current 512Mi limit with healthy headroom.
- CPU requests (currently 100m) align well with the 1000-cluster average (81m). Slightly over-provisioned for 100 clusters but reasonable as a default.
- Memory requests (currently 128Mi) are appropriate up to ~5000 clusters, where the average is 128Mi.
- At 5000 clusters with a 5s poll interval, cycle time exceeds the interval, so Sentinel runs back-to-back with no idle time.

## Recommendations

Right-sized per tier based on observed usage with headroom above peak for unobserved spikes.

| Scale  | Clusters   | CPU (req / limit) | Memory (req / limit) | Rationale                                    |
| ------ | ---------- | ----------------- | -------------------- | -------------------------------------------- |
| Small  | up to 100  | 50m / 150m        | 16Mi / 64Mi          | avg 41m CPU, peak 12Mi mem                   |
| Medium | up to 1000 | 100m / 200m       | 32Mi / 128Mi         | avg 81m CPU, peak 29Mi mem                   |
| Large  | up to 5000 | 125m / 300m       | 175Mi / 256Mi        | avg 98m CPU, peak 154m/163Mi mem             |

At 5000 clusters with a 5s poll interval, Sentinel is saturated with no idle time between cycles. This is the effective maximum for a single instance at the current poll interval. Beyond this point, options include increasing the poll interval to allow idle time between cycles, or splitting the workload across multiple instances using `resourceSelector`. Further profiling would be needed to validate either approach.

## Additional Notes

- **VPA consideration:** A Vertical Pod Autoscaler could automatically lower over-provisioned requests to match actual usage, avoiding the need to manually select a tier. Most useful if cluster count drifts over time and you don't want to revisit sizing.
