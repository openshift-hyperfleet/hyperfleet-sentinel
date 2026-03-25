#!/usr/bin/env python3

import csv
import re
from pathlib import Path
from typing import Optional

_QUANTITY_RE = re.compile(r"^(\d+(?:\.\d+)?)\s*([a-zA-Z]*)$")

_MEM_TO_MI = {"Ki": 1 / 1024, "Mi": 1, "Gi": 1024}


def parse_cpu_millicores(raw: str) -> int:
    """Parse a Kubernetes CPU quantity to millicores (e.g. '100m' -> 100, '1' -> 1000)."""
    m = _QUANTITY_RE.match(raw.strip())
    if not m:
        raise ValueError(raw)
    value, suffix = float(m.group(1)), m.group(2)
    if suffix == "m":
        return round(value)
    if suffix == "n":
        return round(value / 1_000_000)
    if suffix == "":
        return round(value * 1000)
    raise ValueError(raw)


def parse_memory_mi(raw: str) -> int:
    """Parse a Kubernetes memory quantity to MiB (e.g. '128Mi' -> 128, '1Gi' -> 1024)."""
    m = _QUANTITY_RE.match(raw.strip())
    if not m:
        raise ValueError(raw)
    value, suffix = float(m.group(1)), m.group(2)
    if suffix in _MEM_TO_MI:
        return round(value * _MEM_TO_MI[suffix])
    if suffix == "":
        return round(value / (1024 * 1024))
    raise ValueError(raw)


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
        try:
            cpu.append(parse_cpu_millicores(raw_cpu))
            mem.append(parse_memory_mi(raw_mem))
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
        "cpu_low_m":     min(cpu),
        "cpu_high_m":    max(cpu),
        "cpu_avg_m":     round(sum(cpu) / len(cpu)),
        "mem_low_mi":    min(mem),
        "mem_high_mi":   max(mem),
        "mem_avg_mi":    round(sum(mem) / len(mem)),
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
        writer = csv.DictWriter(fh, fieldnames=list(rows[0].keys()))
        writer.writeheader()
        writer.writerows(rows)

    print(f"Wrote {len(rows)} run(s) to {summary}")


if __name__ == "__main__":
    main()