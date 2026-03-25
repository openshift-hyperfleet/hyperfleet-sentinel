#!/usr/bin/env python3

import csv
import re
from pathlib import Path
from typing import Optional


# Matches filenames produced by profile.sh:
#   profile-<cluster_count>-clusters-<YYYYMMDD>-<HHMMSS>.csv
PROFILE_FILENAME_RE = re.compile(
    r"profile-(\d+)-clusters-(\d{4})(\d{2})(\d{2})-(\d{2})(\d{2})(\d{2})\.csv$"
)


def parse_profile(path: Path) -> Optional[dict]:
    """Parse a single profile CSV. Returns a summary dict or None if unusable."""
    m = PROFILE_FILENAME_RE.search(path.name)
    if not m:
        return None

    cluster_count = int(m.group(1))
    timestamp = f"{m.group(2)}-{m.group(3)}-{m.group(4)} {m.group(5)}:{m.group(6)}:{m.group(7)}"

    try:
        with open(path, encoding="utf-8") as fh:
            data = list(csv.DictReader(fh))
    except OSError as e:
        print(f"Skipping {path.name} — could not read file: {e}")
        return None

    cpu, mem = [], []
    skipped = 0
    for row in data:
        raw_cpu = row.get("CPU", "")
        raw_mem = row.get("MEMORY", "")
        if not raw_cpu.endswith("m") or not raw_mem.endswith("Mi"):
            skipped += 1
            continue
        try:
            cpu.append(int(raw_cpu.removesuffix("m")))
            mem.append(int(raw_mem.removesuffix("Mi")))
        except ValueError:
            skipped += 1

    if skipped:
        print(f"  {path.name}: skipped {skipped} malformed row(s)")
    if not cpu:
        print(f"Skipping {path.name} — no valid metric rows")
        return None

    return {
        "cluster_count": cluster_count,
        "timestamp":     timestamp,
        "samples":       len(cpu),
        "cpu_low":       min(cpu),
        "cpu_high":      max(cpu),
        "cpu_avg":       round(sum(cpu) / len(cpu)),
        "mem_low":       min(mem),
        "mem_high":      max(mem),
        "mem_avg":       round(sum(mem) / len(mem)),
    }


def main():
    results_dir = Path(__file__).parent / "results"

    if not results_dir.is_dir():
        print(f"Error: results directory not found: {results_dir}")
        return

    rows = []
    for path in sorted(results_dir.glob("profile-*-clusters-*.csv")):
        result = parse_profile(path)
        if result:
            rows.append(result)

    if not rows:
        print("No valid profile files found.")
        return

    rows.sort(key=lambda r: (r["cluster_count"], r["timestamp"]))

    summary = results_dir / "summary.csv"
    with open(summary, "w", newline="", encoding="utf-8") as fh:
        writer = csv.DictWriter(fh, fieldnames=[
            "cluster_count", "timestamp", "samples",
            "cpu_low", "cpu_high", "cpu_avg",
            "mem_low", "mem_high", "mem_avg",
        ])
        writer.writeheader()
        writer.writerows(rows)

    print(f"Wrote {len(rows)} run(s) to {summary}")


if __name__ == "__main__":
    main()