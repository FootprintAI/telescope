---
name: finops
description: >-
  Turn a telescope report into prioritized FinOps recommendations — rightsizing,
  idle reclamation, and consolidation with dollar-savings estimates. Use when the
  user asks to review, optimize, or cut cloud cost; find idle/over-provisioned
  VMs; rightsize instances; or interpret a telescope scan / report.json.
---

# FinOps recommendations from a telescope report

telescope is **Stage A**: it deterministically collects cloud inventory,
utilization, and cost into a `report.json`. This skill is **Stage B**: it reasons
over that report to produce prioritized, dollar-quantified FinOps actions.
telescope gathers the facts; you supply the judgment. Never edit telescope to
bake recommendations in — the analysis lives here, in the agent.

## Step 1 — Get a report

If the user already has a `report.json`, read it. Otherwise run a scan (from a
telescope checkout or an installed `telescope` binary):

```bash
telescope scan --provider gcp --projects <proj> --pricing --lookback 14d --output json --out report.json
```

- `--provider` — `gcp` | `aws` | `mock` (`mock` gives sample data for a dry run).
- `--pricing` — **required** for savings math; without it the report has no `cost`.
- `--lookback` — default `14d`. Longer windows (`30d`) are more reliable for
  rightsizing; shorter ones can miss weekly peaks.
- Credentials must be **read-only**; telescope never mutates cloud resources.

A worked example lives at `references/sample-report.json`.

## Step 2 — Read the report

Key fields (see `references/sample-report.json` for the full shape):

- `cost.total_monthly_usd` — spend baseline; `cost.unpriced_instances` are
  excluded from savings math (flag them separately).
- `summary.bound_counts` — fleet-wide histogram of workload classes.
- `instances[].instance` — shape: `VCPU`, `MemGB`, `MachineType`, `Disks`, `Labels`.
- `instances[].analysis.bound` — the dominant-resource class (below).
- `instances[].analysis.normalized_p95` — per-dimension p95 utilization in `[0,1]`
  (`cpu`, `mem`, `net`, `disk`); **`-1` means the metric was absent**, not zero.
- `instances[].analysis.notes` — caveats (e.g. missing memory agent).
- `instances[].pricing.monthly_usd` — per-instance cost; the savings denominator.

`bound` values and what they imply:

| bound | meaning | typical action |
|-------|---------|----------------|
| `idle` | every dimension below the idle floor (~0.15) | **strongest savings** — stop if truly unused, else downsize hard |
| `cpu-bound` / `memory-bound` | one resource dominates | rightsize while **preserving the dominant dimension**; pick a matching machine family |
| `network-bound` / `disk-bound` | I/O sensitive | note the I/O constraint before reshaping; shape changes can throttle throughput |
| `balanced` | no dimension dominates | healthy — trim only if all dimensions are low |
| `insufficient-data` | metrics missing/too few samples | **do not cut blindly** — recommend installing the monitoring agent, then rescan |

## Step 3 — Derive recommendations

Reason per instance, then rank by dollar impact.

1. **Idle reclamation.** `bound: idle`, or all `normalized_p95` dims `< 0.15`.
   Confirm it isn't a periodic/batch box via `Labels` and `Max` (not just P95).
   Recommend stop/terminate for orphans; otherwise downsize aggressively.

2. **Rightsizing (headroom).** For each present dimension, headroom `= 1 - p95`.
   Target a post-change p95 of roughly **0.6–0.7** (leaves burst room). Example:
   cpu p95 `0.20` on 4 vCPU ⇒ ~0.8 vCPU used ⇒ 2 vCPU lands p95 ≈ 0.40, 1 vCPU ≈
   0.80 — recommend **2 vCPU**. Preserve the `bound` dimension: never shrink memory
   on a `memory-bound` box to hit a CPU target. Ignore any dimension with p95 `-1`.

3. **Family fit.** `memory-bound` with low cpu ⇒ memory-optimized family
   (high mem:vCPU). `cpu-bound` with low mem ⇒ compute-optimized. Keep the
   provider/region from `instance` in the suggested target shape.

4. **Data-quality gate.** If `notes` flags a missing metric or `bound` is
   `insufficient-data`, lead with "instrument, then rescan" — do not quantify
   savings you can't support.

5. **Consolidation (fleet view).** Many `idle`/`balanced` low-util instances in the
   same `Region`/`Project` are bin-packing candidates. Surface the opportunity;
   telescope's report is the handoff for the downstream consolidation planner.

**Savings estimate.** Per action, `est. monthly savings ≈ pricing.monthly_usd ×
fraction_removed` (1.0 for stop; e.g. 4→2 vCPU ≈ 0.5 when cost scales ~linearly
with size). State the assumption; call estimates approximate — list price only.

## Step 4 — Present it

Lead with the fleet total (`cost.total_monthly_usd`) and total estimated monthly
savings. Then a table ranked by savings, most impactful first:

| Instance | Region | Type | bound | Monthly $ | Recommendation | Est. saving/mo | Confidence |
|----------|--------|------|-------|-----------|----------------|----------------|------------|

Rules: quantify only priced instances; never recommend a cut on
`insufficient-data`; call out `unpriced_instances` and missing-metric instances as
a separate "needs data" list. Confidence = High for `idle` with full metrics,
Low when samples are few or a dimension is absent. Recommendations are advisory —
telescope and this skill never modify cloud resources.
