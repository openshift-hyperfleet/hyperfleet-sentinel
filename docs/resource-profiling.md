# Sentinel Profiling Results

**Status**: Active
**Owner**: HyperFleet Team
**Last Updated**: 2026-03-26

[HYPERFLEET-556](https://issues.redhat.com/browse/HYPERFLEET-556) — Validate resource limits and requests against actual Sentinel consumption.

**Audience:** Platform operators and SREs sizing Sentinel deployments.

## Goal

Validate that current resource defaults are appropriately sized under realistic load. Current defaults: CPU 100m request / 500m limit, memory 128Mi request / 512Mi limit. Ensure limits prevent noisy-neighbor issues without causing OOM kills or CPU throttling.

## Setup

- **Poll interval:** 5s (chart default)
- **Duration:** 15 min per scenario
- **Sample interval:** 60s (`kubectl top`)
- **Tracing:** Disabled
- **Mock API:** In-cluster mock-hyperfleet-api
- **Cluster counts:** 100, 1000, 5000 (number of cluster resources returned by the API per poll)
- **Tooling:** [`test/profiling/README.md`](../test/profiling/README.md) — `profile.sh` to capture samples, `analyze.py` to aggregate

## Results

| Clusters | CPU                 | Memory               |
| -------- | ------------------- | -------------------- |
| 100      | 43–63m (avg 48m)    | 11–13Mi (avg 12Mi)   |
| 1000     | 84–106m (avg 96m)   | 25–40Mi (avg 28Mi)   |
| 5000     | 80–172m (avg 101m)  | 84–147Mi (avg 96Mi)  |

## Findings

- CPU stays well under the current 500m limit at all scales. Peak was 172m at 5000 clusters. No risk of CPU throttling.
- Memory grows with cluster count. The 5000-cluster run ranges between ~84Mi and ~147Mi. Peak 147Mi is within the current 512Mi limit with healthy headroom.
- CPU requests (currently 100m) are tight at 1000 clusters where the average is 96m and peaks hit 106m. Over-provisioned for 100 clusters (avg 48m) but reasonable as a default.
- Memory requests (currently 128Mi) are appropriate up to ~1000 clusters. At 5000 clusters the average is 96Mi but peaks reach 147Mi, exceeding 128Mi.
- At 5000 clusters with a 5s poll interval, cycle time exceeds the interval, so Sentinel runs back-to-back with no idle time.

## Recommendations

Right-sized per tier based on observed usage with headroom above peak for unobserved spikes. **Select the tier that covers your maximum cluster count.** For example, if managing 500 clusters, use the Medium tier.

| Scale  | Clusters    | CPU (req / limit) | Memory (req / limit) | Rationale                                    |
| ------ | ----------- | ----------------- | -------------------- | -------------------------------------------- |
| Small  | 1–100       | 50m / 150m        | 16Mi / 64Mi          | avg 48m CPU, peak 13Mi mem                   |
| Medium | 101–1000    | 125m / 250m       | 32Mi / 128Mi         | avg 96m CPU, peak 40Mi mem                   |
| Large  | 1001–5000   | 125m / 300m       | 175Mi / 256Mi        | avg 101m CPU, peak 147Mi mem                 |

At 5000 clusters with a 5s poll interval, Sentinel is saturated with no idle time between cycles. This is the effective maximum for a single instance at the current poll interval. Beyond this point, options include increasing the poll interval to allow idle time between cycles, or splitting the workload across multiple instances using `resourceSelector`. Further profiling would be needed to validate either approach.

## Additional Notes

- **VPA consideration:** A Vertical Pod Autoscaler could automatically lower over-provisioned requests to match actual usage, avoiding the need to manually select a tier. Most useful if cluster count drifts over time and you don't want to revisit sizing.

## See Also

- [Metrics Documentation](metrics.md) - Resource consumption metrics
- [Runbook](runbook.md) - Operational guidance and troubleshooting
- [Profiling Tooling](../test/profiling/README.md) - How to run your own profiles
