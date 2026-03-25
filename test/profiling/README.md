# Profiling

Measures Sentinel CPU and memory usage over time using `kubectl top`.

## Prerequisites

- Python 3
- `kubectl` configured with access to the `hyperfleet` namespace
- Sentinel deployed in the `hyperfleet` namespace

## Collect a Profile

Samples `kubectl top` every 60 seconds and writes results to a timestamped CSV in `results/`.

| Variable        | Default | Description                                          |
|-----------------|---------|------------------------------------------------------|
| `CLUSTER_COUNT` | `100`   | Cluster count (used in the output filename)          |
| `DURATION_MIN`  | `5`     | How long to record, in minutes (= number of samples) |

```bash
CLUSTER_COUNT=1000 DURATION_MIN=10 sh ./test/profiling/profile.sh
```

Output: `results/profile-<cluster_count>-clusters-<timestamp>.csv`

```text
TIME,POD,CPU,MEMORY
11:42:49,hyperfleet-sentinel-clusters-...-vmz62,54m,12Mi
11:43:49,hyperfleet-sentinel-clusters-...-vmz62,1m,12Mi
```

## Analyze Results

Aggregates all CSVs in `results/` into a single summary with min/max/avg CPU and memory per run.

```bash
python3 ./test/profiling/analyze.py
```

Output: `results/summary.csv`

```text
Cluster Count,Date,Samples,CPU Low (m),CPU High (m),CPU Avg (m),Memory Low (Mi),Memory High (Mi),Memory Avg (Mi)
100,2026-03-25 11:46:06,5,36,54,41,11,12,12
1000,2026-03-25 11:59:14,5,74,88,81,26,29,27
5000,2026-03-25 12:21:07,5,71,154,98,85,163,128
```
