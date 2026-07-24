# PRD: Savings Score — turn the telescope report into a qualification instrument

**Date:** 2026-07-24
**Status:** draft
**Owner:** devops@footprint-ai.com

## Problem

Telescope produces a detailed but *flat* report: per-instance utilization, bound
classification, and (with `--pricing`) a list-cost roll-up. Nothing in it answers
the one question a prospect — or a FootprintAI salesperson — actually asks first:
**"how much money is being wasted, and is this account worth a conversation?"**

Two costs follow from that gap:

- **The report doesn't self-qualify leads.** When a prospect's report reaches
  FootprintAI's cloud service, a human has to read the whole thing to judge whether
  the account has meaningful waste. There's no single number to sort inbound by.
- **The report doesn't motivate forwarding.** A prospect who runs `telescope scan`
  sees a table of percentages. There's no headline that makes them think "I should
  send this to someone who can cut this bill" — which is the entire funnel premise.

**Evidence:** No usage/funnel data exists yet (the tool is pre-instrumentation) —
this is stated as an assumption to validate, not measured fact. The problem is
inferred from the product design: the README frames the report as "the handoff …
to Footprint AI's cloud service," yet the artifact contains no waste headline and no
qualification signal. **Assumption to validate:** prospects don't forward, and
salespeople can't triage, because there's no top-line dollar-waste number.

Separately, the `/telescope` lead-magnet link 404s. A funnel whose entry point is
broken leaks every top-of-funnel visitor before the report is ever run. This link
does **not** live in the telescope repo — it belongs to the sibling `website/`
property — so it is tracked here but delivered there (see Later phases / Open
questions).

## Target user

Two roles, one artifact:

- **Primary — the prospect / customer platform engineer** who runs `telescope scan`
  on their own cloud. Job-to-be-done: *quickly see whether their cloud spend has
  enough waste to be worth acting on*, in one number they trust enough to forward.
- **Secondary — the FootprintAI sales/solutions engineer** who receives `report.json`.
  Job-to-be-done: *triage inbound reports by recoverable dollars* without reading
  every line.

## Success metrics

All go-to-market baselines are currently **unknown — instrument first**; the CLI
cannot observe forwarding directly, so the honest measurable proxy is inbound
`report.json` volume at the cloud service plus the qualifier the Score provides.

| Metric | Baseline | Target |
|--------|----------|--------|
| Inbound reports with `recoverable_monthly_usd ≥ $500` (qualified) / total inbound | unknown | ≥ 40% of inbound are auto-qualifiable without human read |
| Median sales-triage time per inbound report | unknown (full manual read) | < 30s (read the Score line only) |
| Prospect forward rate (proxy: distinct accounts submitting a report) | unknown | establish baseline this sprint, +25% next |

## MVP scope — the core journey

**The one journey:** a platform engineer runs `telescope scan --pricing`, and the
report leads with a **Savings Score** block — total spend, share of spend that is
under-utilized, share of fleet running always-on, and estimated recoverable dollars
— rendered as the first thing they (and later a salesperson) see.

The Score appears only when `--pricing` is on (it is dollar-denominated). Without
pricing, the utilization/always-on percentages still render; dollar fields are
omitted, not zero-filled.

### P0 stories

**Story 1 — Savings Score in the report schema.**
As a sales engineer, I want a machine-readable `savings_score` object in
`report.json` so that inbound reports can be auto-triaged by recoverable dollars.
**Acceptance criteria:**
- [ ] `report.json` gains a top-level `savings_score` object with:
      `total_monthly_usd`, `underutilized_spend_pct`, `always_on_instance_pct`,
      `recoverable_monthly_usd`, and a `basis` sub-object documenting the thresholds
      and formula used.
- [ ] Dollar fields are omitted (not `0`) when `--pricing` is off; percentage fields
      still populate.
- [ ] `SchemaVersion` is bumped and the change noted in the report contract.
**Priority:** P0

**Story 2 — Spend and under-utilized share.**
As a platform engineer, I want to see my total monthly list spend and the % of it
going to under-utilized instances so that I can gauge waste at a glance.
**Acceptance criteria:**
- [ ] `total_monthly_usd` = sum of monthly cost across priced instances (matches the
      existing `CostSummary.TotalMonthlyUSD`).
- [ ] `underutilized_spend_pct` = (monthly spend on instances whose top normalized
      p95 utilization < 30%) ÷ total priced monthly spend, in [0,100].
- [ ] The 30% threshold is a named constant, distinct from the existing 15% idle
      floor, and surfaced in `savings_score.basis`.
- [ ] Instances with `insufficient-data` are excluded from the numerator and counted
      in a `basis.excluded_no_data` count (not silently dropped).
**Priority:** P0

**Story 3 — Always-on share (new uptime signal).**
As a sales engineer, I want the % of instances running effectively 24/7 so that I
can distinguish reclaimable steady-state waste from legitimate bursty workloads.
**Acceptance criteria:**
- [ ] Instance inventory gains an uptime signal sourced from GCP
      `Instance.CreationTimestamp` and AWS `LaunchTime` (both already exposed by the
      SDKs; neither read today).
- [ ] `always_on_instance_pct` = share of listed instances whose observed running
      time implies > 720h/month (i.e. continuously running across the window).
- [ ] The derivation method and its limitation are recorded in `savings_score.basis`
      (see Open questions — creation-timestamp proxy vs. metric-sample-density proxy).
- [ ] Mock provider produces a plausible non-zero value so the feature is
      demonstrable offline.
**Priority:** P0

**Story 4 — Estimated recoverable dollars (conservative, transparent).**
As a prospect, I want a single "you could recover ~$X/month" number I trust so that
I have a reason to forward the report.
**Acceptance criteria:**
- [ ] `recoverable_monthly_usd` = 100% of monthly spend on `idle` instances + a
      configurable rightsizing fraction (default 0.5) of monthly spend on instances
      that are under-utilized (<30%) but not idle.
- [ ] The exact formula, the rightsizing fraction, and the instance counts feeding
      each term appear in `savings_score.basis` so the number is auditable.
- [ ] The number is never higher than `total_monthly_usd`; Spot/unpriced instances
      contribute $0 to recoverable (no overstatement).
- [ ] Copy framing is "estimated" everywhere it renders.
**Priority:** P0

**Story 5 — Savings Score as the report headline.**
As a platform engineer, I want the Score shown first in the human-readable outputs
so that the waste number is the thing I see (and forward).
**Acceptance criteria:**
- [ ] Table and Markdown renderers print a Savings Score header block before the
      per-instance detail; CSV/XLSX gain a Savings Score summary row/sheet.
- [ ] A one-line headline reads e.g. `Savings Score: ~$1,306/mo recoverable
      (42% of spend under-utilized, 31 of 40 instances always-on)`.
- [ ] When `--pricing` is off, the headline shows the percentage signals and a
      "run with --pricing for dollar estimates" hint.
**Priority:** P0

## Later phases

- **P1 — Instrument forwarding.** Add a lightweight, privacy-respecting signal (e.g.
  a report ID) so the cloud service can measure submit/forward rate against the
  success metrics above. *Rationale: needed to move baselines off "unknown," but not
  required for the Score to deliver value.*
- **P2 — Waste breakdown by team/label.** Per-label recoverable-dollar rollup for
  larger accounts. *Rationale: valuable for enterprise triage, over-scoped for MVP.*
- **P2 — Confidence band on recoverable $.** Range instead of point estimate.

## Out of scope

- **Actual rightsizing/consolidation recommendations.** That is the paid Containarium
  cloud service's job by design (README: this repo "contains no recommendation,
  bin-packing, or pricing logic" beyond list-price metadata). The Score estimates
  waste; it does not prescribe the fix. Kept cut to preserve the product boundary.
- **Non-list (negotiated/CUD/committed-use) pricing.** The Score uses on-demand list
  price like the rest of `--pricing`; real invoices differ. Stated in `basis`.
- **Historical trending / month-over-month.** Single-snapshot only for MVP.
- **The `/telescope` 404 fix.** Descoped by the product owner for this sprint to keep
  focus on the Savings Score. The broken link lives in the sibling `website/` repo,
  not telescope; revisit as a separate funnel task later.

## Open questions & assumptions

1. **Always-on derivation (Story 3).** Two candidate proxies: (a) `CreationTimestamp`/
   `LaunchTime` age ≥ 1 month AND currently running; (b) metric-sample density across
   the lookback window (continuous samples ⇒ continuously running). (a) is simpler but
   an instance created long ago could have been stopped/started; (b) directly measures
   running-during-window but only covers the lookback, not a full month. **Assumption:**
   proxy (a) gated on "currently running" is good enough for a qualification signal;
   validate against a real account during the GCP/AWS smoke test.
2. **Recoverable formula (Story 4).** Default rightsizing fraction 0.5 is an assumption,
   not measured. Validate against a handful of real reports where the Containarium
   service produced an actual recommendation, and tune. Conservative-by-default is the
   rule: better to under-promise in a lead magnet.
3. **30% threshold (Story 2).** Chosen as an intuitive "clearly under-used" line above
   the 15% idle floor. Confirm it's the number sales wants to anchor on.
4. **The `/telescope` 404.** Confirm the exact broken URL and that it lives in the
   `website/` repo (a sibling exists in the workspace); needs a maintainer of that repo
   to action. The fix cannot be made from the telescope codebase.
5. **Success-metric instrumentation.** The CLI cannot observe forwarding; baselines
   depend on the cloud service counting inbound reports. Confirm that measurement path
   exists before committing to the forward-rate target.
