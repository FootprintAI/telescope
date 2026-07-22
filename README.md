# telescope

`telescope` is a Go CLI that connects to a customer's cloud with a
**customer-provided, read-only service account**, inventories their compute
(GCE/EC2 instances and GKE/EKS nodepools), pulls **live utilization metrics**,
and classifies each workload as **CPU- / memory- / network- / disk-bound** (or
**idle**). It emits a **shareable report** (CSV / Excel / Markdown / JSON).

The report is the handoff: the customer runs `telescope scan` and passes the
report to Footprint AI's **cloud service**, which privately generates the
[Containarium](https://github.com/FootprintAI/Containarium) consolidation
recommendation and cost saving. This repo is the customer-side data collector
only — it contains no recommendation, bin-packing, or pricing logic.

```
telescope (customer, read-only SA):  Provider(GCP|AWS) ─► Inventory ─► Metrics ─► Analyze ─► report.{csv,xlsx,md,json}
cloud service (Footprint AI):                                                         report.json ─► recommendation
```

## Install / build

```bash
go build -o telescope ./cmd/telescope
```

## Usage

```bash
# Try it with no cloud creds (synthetic fixtures):
telescope scan --provider mock

# Real GCP:
telescope scan --provider gcp \
  --projects my-project-a,my-project-b \
  --regions us-central1,europe-west1 \
  --credentials ./readonly-sa.json \
  --lookback 14d \
  --output csv --out report.csv

# Real AWS (uses AWS_PROFILE / default chain, or --credentials <file>):
telescope scan --provider aws \
  --regions us-east-1,eu-west-1 \
  --lookback 14d \
  --output csv --out report.csv
```

Flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `--provider` | `mock` | `gcp` \| `aws` \| `mock` |
| `--projects` | — | comma-separated GCP projects (required for gcp; ignored for aws) |
| `--regions` | all | comma-separated regions to include |
| `--credentials` | ADC | GCP: SA JSON path. AWS: shared-credentials file path. Else default chain |
| `--lookback` | `14d` | metrics window, e.g. `14d`, `24h` |
| `--output` | `table` | `table` \| `csv` \| `xlsx` \| `markdown` \| `json` |
| `--out` | stdout | write to a file (required for `xlsx`) |
| `--pricing` | off | attach live on-demand price metadata (best-effort) |

The **JSON** report is the machine-readable handoff for the cloud service;
CSV / Excel / Markdown are human-shareable deliverables.

## Pricing metadata (`--pricing`)

With `--pricing`, each instance is annotated with its on-demand list price and
the report gains a cost roll-up (`cost` in JSON, `$/hr` column + Summary line in
the other formats):

```
On-demand list cost: $1.79/hr, $1306.70/mo (6 priced, 0 unpriced)
```

Prices are fetched live and best-effort — a pricing failure warns but never
fails the scan; unresolved instances are marked unpriced. Sources
(`pricing.source` in the report):

- **`aws-pricing-api`** — AWS Price List Query API (`GetProducts`, Linux/Shared/
  on-demand). Needs `pricing:GetProducts`.
- **`gcp-billing-catalog`** — Cloud Billing Catalog, decomposing predefined
  types into per-vCPU + per-GB SKUs. Needs the Cloud Billing API enabled;
  **custom** machine types are left unpriced.
- **`static`** — embedded table (mock provider / offline demo).

## How workloads are classified

For each instance, over the lookback window, telescope computes p50/p95/max for
CPU %, memory %, network throughput, and disk IOPS, normalizes each to its
capacity, then picks the dominant dimension:

- one dimension clearly on top → `cpu-bound` / `memory-bound` / `network-bound` / `disk-bound`
- everything under the idle floor → `idle` (prime consolidation candidate)
- no clear leader → `balanced`
- no metrics (or memory metric missing) → `insufficient-data`

> **Note:** GCE/EC2 **memory** metrics require a monitoring agent (GCP Ops Agent
> / CloudWatch agent). When absent, telescope flags it rather than guessing.

## Metrics window

telescope pulls the trailing `--lookback` window (default **14 days**) from the
monitoring backend at **5-minute alignment**, and reports p50/p95/max per
dimension. Shorten it for a quick look (`--lookback 24h`) or lengthen it
(`--lookback 30d`), within the backend's retention.

## GCP setup

Use the helper script to create a read-only service account and key:

```bash
./scripts/create-readonly-sa.sh --projects my-project-a,my-project-b
# -> writes ./telescope-sa.json and prints the exact scan command
```

It grants only these read-only roles:

- `roles/compute.viewer` — Compute Engine instances, machine types, disks
- `roles/container.viewer` — GKE clusters/nodepools
- `roles/monitoring.viewer` — Cloud Monitoring metrics

Prefer no downloaded key? Run with `--no-key` and use Application Default
Credentials (`gcloud auth application-default login`) or Workload Identity.

## AWS setup

Credentials come from `--credentials <shared-file>` or the default AWS chain
(`AWS_PROFILE`, env vars, SSO, or an instance/role identity). `--projects` is
ignored (the account is fixed by the credentials); `--regions` limits the scan,
otherwise every enabled region is queried.

Minimal read-only IAM (attach `ReadOnlyAccess`, or this focused policy):

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": [
      "ec2:DescribeInstances", "ec2:DescribeInstanceTypes",
      "ec2:DescribeVolumes", "ec2:DescribeRegions",
      "eks:ListClusters", "eks:ListNodegroups", "eks:DescribeNodegroup",
      "cloudwatch:GetMetricData", "cloudwatch:ListMetrics",
      "pricing:GetProducts"
    ],
    "Resource": "*"
  }]
}
```

EC2 **memory** requires the CloudWatch agent publishing `mem_used_percent` to
the `CWAgent` namespace; telescope matches it with a `SEARCH` expression so extra
agent dimensions still resolve. When absent, memory is flagged, not guessed.

### Capacity ceilings (network & disk)

To classify network- and disk-bound workloads, telescope normalizes throughput
against real ceilings rather than guesses:

- **Network:** GCP — `min(vCPU × 2 Gbps, family cap)` (e2 16, n2/c2 32, c3/c4
  100, a3 200, …). AWS — the instance type's peak/baseline bandwidth, or the
  parsed `NetworkPerformance` string.
- **Disk IOPS:** GCP — provisioned-IOPS disks (pd-extreme, hyperdisk) use their
  provisioned value; size-scaled types use baseline read IOPS (pd-ssd 30/GB,
  pd-balanced 6/GB, pd-standard 0.75/GB, capped). AWS — io1/io2/gp3 use
  provisioned IOPS; gp2 scales at 3 IOPS/GB (100–16000). Unknown/throughput
  types are left unset so the disk dimension is skipped, not guessed.

## Project layout

```
cmd/telescope/           entrypoint
internal/cli/            scan command
internal/provider/       Provider interface
internal/provider/mock/  synthetic fixtures (offline demo/tests)
internal/provider/gcp/   Compute + GKE inventory, Cloud Monitoring, capacity ceilings
internal/provider/aws/   EC2 + EKS inventory, CloudWatch, capacity ceilings
internal/pricing/        live price metadata (aws price list, gcp billing catalog, static)
internal/metricsutil/    shared p50/p95/max summarization
internal/model/          shared types
internal/analyze/        bound classification
internal/report/         report schema + csv/xlsx/md/json renderers
scripts/                 create-readonly-sa.sh (GCP)
```

## Roadmap

- [x] Core pipeline, mock provider, analysis, CSV/Excel/Markdown/JSON reports
- [x] **GCP provider**: Compute Engine + GKE inventory + Cloud Monitoring metrics
- [x] **AWS provider**: EC2 + EKS inventory + CloudWatch metrics
- [x] Per-family/instance NIC bandwidth caps; provisioned-disk IOPS ceilings
- [x] Read-only SA bootstrap script (GCP)
- [x] Live pricing metadata in the report (`--pricing`)

## Testing

```bash
go test ./...
```

## License

Licensed under the [Apache License 2.0](LICENSE).
