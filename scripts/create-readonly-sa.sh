#!/usr/bin/env bash
#
# create-readonly-sa.sh — provision a READ-ONLY service account for telescope.
#
# It creates one service account and grants it the three viewer roles telescope
# needs, across one or more projects, then (optionally) downloads a JSON key.
#
#   Roles granted (read-only):
#     roles/compute.viewer      GCE instances + machine types
#     roles/container.viewer    GKE clusters / nodepools
#     roles/monitoring.viewer   Cloud Monitoring metrics
#
# Usage:
#   ./create-readonly-sa.sh --projects PROJECT[,PROJECT2,...] [options]
#
# Options:
#   --projects   Comma-separated projects to grant read access on (required).
#   --sa-project Project that OWNS the service account (default: first --projects).
#   --name       Service-account id (default: telescope-readonly).
#   --key        Output path for the JSON key (default: ./telescope-sa.json).
#   --no-key     Skip key creation (use with Workload Identity / impersonation).
#   -h, --help   Show this help.
#
# Env vars mirror the flags: PROJECTS, SA_PROJECT, SA_NAME, KEY_FILE, NO_KEY.
#
set -euo pipefail

PROJECTS="${PROJECTS:-}"
SA_PROJECT="${SA_PROJECT:-}"
SA_NAME="${SA_NAME:-telescope-readonly}"
KEY_FILE="${KEY_FILE:-telescope-sa.json}"
NO_KEY="${NO_KEY:-}"

die()  { echo "error: $*" >&2; exit 1; }
info() { echo "==> $*"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --projects)   PROJECTS="$2"; shift 2 ;;
    --sa-project) SA_PROJECT="$2"; shift 2 ;;
    --name)       SA_NAME="$2"; shift 2 ;;
    --key)        KEY_FILE="$2"; shift 2 ;;
    --no-key)     NO_KEY=1; shift ;;
    -h|--help)    sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *)            die "unknown argument: $1 (try --help)" ;;
  esac
done

command -v gcloud >/dev/null 2>&1 || die "gcloud CLI not found. Install the Google Cloud SDK first."
[[ -n "$PROJECTS" ]] || die "--projects is required (comma-separated). See --help."

# Split comma list.
IFS=',' read -r -a PROJECT_ARR <<< "$PROJECTS"
[[ -n "$SA_PROJECT" ]] || SA_PROJECT="${PROJECT_ARR[0]}"

# Confirm the caller is authenticated.
ACTIVE_ACCT="$(gcloud auth list --filter=status:ACTIVE --format='value(account)' 2>/dev/null || true)"
[[ -n "$ACTIVE_ACCT" ]] || die "no active gcloud auth. Run: gcloud auth login"
info "Authenticated as: $ACTIVE_ACCT"

SA_EMAIL="${SA_NAME}@${SA_PROJECT}.iam.gserviceaccount.com"

# 1. Create the service account (idempotent).
if gcloud iam service-accounts describe "$SA_EMAIL" --project "$SA_PROJECT" >/dev/null 2>&1; then
  info "Service account already exists: $SA_EMAIL"
else
  info "Creating service account: $SA_EMAIL"
  gcloud iam service-accounts create "$SA_NAME" \
    --project "$SA_PROJECT" \
    --display-name "telescope read-only collector"
fi

# 2. Grant the read-only roles on every target project.
ROLES=(roles/compute.viewer roles/container.viewer roles/monitoring.viewer)
for proj in "${PROJECT_ARR[@]}"; do
  proj="$(echo "$proj" | xargs)" # trim
  [[ -n "$proj" ]] || continue
  for role in "${ROLES[@]}"; do
    info "Granting $role on $proj"
    gcloud projects add-iam-policy-binding "$proj" \
      --member "serviceAccount:${SA_EMAIL}" \
      --role "$role" \
      --condition=None \
      --quiet >/dev/null
  done
done

# 3. Create a key (unless --no-key).
if [[ -n "$NO_KEY" ]]; then
  info "Skipping key creation (--no-key)."
else
  if [[ -e "$KEY_FILE" ]]; then
    die "key file '$KEY_FILE' already exists; move it or pass --key <path>."
  fi
  info "Creating key: $KEY_FILE"
  gcloud iam service-accounts keys create "$KEY_FILE" --iam-account "$SA_EMAIL"
  chmod 600 "$KEY_FILE"
  echo
  echo "  ⚠  Keep $KEY_FILE secret. Delete it when the scan is done:"
  echo "     gcloud iam service-accounts keys list --iam-account $SA_EMAIL"
fi

echo
info "Done. Run a scan with:"
echo "     telescope scan --provider gcp \\"
echo "       --projects ${PROJECTS} \\"
if [[ -z "$NO_KEY" ]]; then
  echo "       --credentials ${KEY_FILE} \\"
fi
echo "       --output csv --out report.csv"
