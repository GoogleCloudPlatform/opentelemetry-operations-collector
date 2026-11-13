# Telemetry Policy smoke test setup

`TestTelemetryPoliciesSmoke` (`../telemetry_policy_smoke_test.go`) installs the
packaged `otelcol-google` service on a GCE VM (Linux or Windows) and configures
the service to run solely with
`--config=googlecontrolplane:xds://$TELEMETRY_POLICY_CONTROL_PLANE_ENDPOINT?gcp.fleet_id=$FLEET_ID&region=$REGION`
(without any local `config.yaml`): via a systemd drop-in on Linux, and via
[`configure_windows.ps1`](configure_windows.ps1) updating the service's
`ImagePath` registry value on Windows. It then polls Cloud Monitoring until the
collector's self-o11y gauge `prometheus.googleapis.com/gcp.policy.set.active/gauge`
reports the pre-provisioned OS-specific PolicySet (`gcp.policy.set.id`) at the
expected revision. That gauge only updates after the collector has evaluated,
applied, and ACKed a revision received from the Control Plane, proving the
Control Plane to Data Plane loop.

Once the PolicySet is active, the test writes [`sample_logs.log`](sample_logs.log)
over SSH into the watched directory (`/var/log/gboc-smoke/test.log` on Linux,
`C:\gboc-smoke\test.log` on Windows), which is tailed by
`filelog/googlecontrolplane` synthesized from the PolicySet's
`fileLogSourcePolicy`. It then verifies in Cloud Logging that:
- the kept line (`gboc-smoke=keep`) arrived **and** carries `labels.gboc_smoke="true"`
  added by `logTransformPolicy`, and
- both lines targeted by `logFilterPolicy` (`gboc-smoke=drop` and
  `smoke-test=other`) were dropped.

Linux and Windows collectors belong to separate fleets and receive OS-specific
PolicySets (`policyset.json` for `/var/log/gboc-smoke/*.log` on Linux, and
`policyset_windows.json` for `%SystemDrive%/gboc-smoke/*.log` on Windows). The
same PolicySets are used for both on-GCP and off-GCP execution.

The test assumes the fleets and PolicySets already exist and have been rolled
out. It never creates, updates, or deletes them.

## Pre-provisioned fleets, PolicySets, and X.509 WIF resources

On-GCP runs target `stackdriver-test-143416` (`228437877067`), and off-GCP runs
target `<OFF_GCP_PROJECT_ID>` (`406580743452`, positioned outside the
`google.com` organization so external X.509 WIF pools are permitted). Both
projects hold identical Linux (`gboc-smoke-fleet` / `gboc-smoke-ps`) and Windows
(`gboc-smoke-fleet-windows` / `gboc-smoke-ps-windows`) definitions in
`asia-east1` against staging Telemetry Management and staging Telemetry Director
(`staging-telemetrydirector.asia-east1.rep.sandbox.googleapis.com:443`):

| Resource | Name |
|---|---|
| On-GCP Project | `stackdriver-test-143416` (number `228437877067`, Linux rev `68668fd3`, Windows rev `86f9ce08`) |
| Off-GCP Project | `<OFF_GCP_PROJECT_ID>` (number `406580743452`, Linux rev `1b20a331`, Windows rev `3d90357c`) |
| Linux Fleet | `projects/<project>/locations/asia-east1/fleets/gboc-smoke-fleet` |
| Linux PolicySet | `projects/<project_number>/locations/asia-east1/policySets/gboc-smoke-ps` |
| Windows Fleet | `projects/<project>/locations/asia-east1/fleets/gboc-smoke-fleet-windows` |
| Windows PolicySet | `projects/<project_number>/locations/asia-east1/policySets/gboc-smoke-ps-windows` |
| Workload Identity Pool (`<OFF_GCP_PROJECT_ID>`) | `projects/406580743452/locations/global/workloadIdentityPools/gboc-x509-pool` |
| X.509 Provider (`<OFF_GCP_PROJECT_ID>`) | `projects/406580743452/locations/global/workloadIdentityPools/gboc-x509-pool/providers/gboc-x509-prov` |
| Principal (`<OFF_GCP_PROJECT_ID>`) | `principal://iam.googleapis.com/projects/406580743452/locations/global/workloadIdentityPools/gboc-x509-pool/subject/gboc-smoke-workload` |
| Workload Certificate Secret (`<OFF_GCP_PROJECT_ID>`) | `projects/406580743452/secrets/gboc-x509-leaf-cert` |
| Workload Key Secret (`<OFF_GCP_PROJECT_ID>`) | `projects/406580743452/secrets/gboc-x509-leaf-key` |

> **Note on `stackdriver-test-143416` staging rollouts:** New staging rollouts in
> `stackdriver-test-143416` are temporarily blocked on staging Control Plane
> migration state; until cleared, on-GCP runs in `stackdriver-test-143416` can
> pass `FLEET_ID=projects/406580743452/locations/asia-east1/fleets/gboc-smoke-fleet`
> and `WINDOWS_FLEET_ID=projects/406580743452/locations/asia-east1/fleets/gboc-smoke-fleet-windows`
> (with `TELEMETRY_POLICY_EXPECTED_REVISION=1b20a331` and `TELEMETRY_POLICY_EXPECTED_REVISION_WINDOWS=3d90357c`),
> then re-run `./setup_policyset.sh stackdriver-test-143416` once cleared.

Both the X.509 WIF principal (used off-GCP) and each project's default Compute
Engine service account (used on-GCP, and detached before starting the collector
in off-GCP mode) have `roles/telemetry.writer`, `roles/logging.logWriter`,
`roles/monitoring.metricWriter`, `roles/trafficdirector.client`, and
`roles/viewer`. The Compute Engine service account also has
`roles/storage.objectViewer` on the test transfers bucket so VMs can download
collector packages during initial installation.

The workload certificate (`CN=gboc-smoke-workload`) signed by the CA in the
provider's trust store (`CN=gboc-smoke-x509-ca`) and its private key are stored
in Cloud Secret Manager in `<OFF_GCP_PROJECT_ID>` (`gboc-x509-leaf-cert` and
`gboc-x509-leaf-key`). Test runners and developers running `OFF_GCP=true` need
`roles/secretmanager.secretAccessor` on those secrets (the WIF principal itself
does not have Secret Manager access); `WIF_DIR` can optionally override them
with local `leaf.pem` and `leaf.key` files. Re-running `setup_policyset.sh`
reuses the existing provider and secrets unless `ROTATE_WIF_CERTS=true` is set.

## Recreating the resources

All resources above are pre-created. [`setup_policyset.sh`](setup_policyset.sh)
recreates or updates them idempotently and is not run by CI.

```
./setup_policyset.sh PROJECT_ID [LOCATION] [WIF_PROJECT_ID] [WIF_OUT_DIR]
```

| Argument | Example | What it sets up |
|---|---|---|
| `PROJECT_ID` (required) | `stackdriver-test-143416` or `<OFF_GCP_PROJECT_ID>` | Opts the project into the staging Telemetry Management and Telemetry Director APIs, grants service-agent and Compute SA roles, creates or updates both Linux (`gboc-smoke-fleet` / `gboc-smoke-ps`) and Windows (`gboc-smoke-fleet-windows` / `gboc-smoke-ps-windows`) fleets and PolicySets, and rolls the latest revisions out. |
| `LOCATION` | `asia-east1` (default) | Location of the fleets and PolicySets. |
| `WIF_PROJECT_ID` (optional) | `<OFF_GCP_PROJECT_ID>` | Creates or updates the X.509 WIF pool and provider, generates the CA, `trust_store.json`, and workload certificates (storing the leaf cert and key in Secret Manager when first created or when `ROTATE_WIF_CERTS=true`), and grants the IAM roles above. Skipped when omitted. |
| `WIF_OUT_DIR` | `/tmp/gboc-smoke-wif` (default) | Working directory where generated certificates and `trust_store.json` are written when creating or rotating certificates. |

To recreate the on-GCP fleets and PolicySets:

```
./setup_policyset.sh stackdriver-test-143416
```

To recreate both fleets, PolicySets, and X.509 WIF resources in `<OFF_GCP_PROJECT_ID>`:

```
./setup_policyset.sh <OFF_GCP_PROJECT_ID> asia-east1 <OFF_GCP_PROJECT_ID>
```

Any PolicySet update creates a new revision. Update
`TELEMETRY_POLICY_EXPECTED_REVISION` and
`TELEMETRY_POLICY_EXPECTED_REVISION_WINDOWS` and the table above to match.

A newly created fleet can take several minutes to become visible to Telemetry
Director. Until then the collector logs
`NotFound desc = Traffic Director configuration was not found`.

## Inspecting the resources

```
P=<OFF_GCP_PROJECT_ID>
PN=406580743452
BASE=https://staging-telemetrymanagement.sandbox.googleapis.com/v1alpha/projects/$PN/locations/asia-east1
HDRS=(-H "Authorization: Bearer $(gcloud auth print-access-token)" -H "X-Goog-User-Project: $P" -H "X-Goog-Visibilities: TRUSTED_TESTER_COLLECTORS")

curl -sS "${HDRS[@]}" "$BASE/fleets/gboc-smoke-fleet"
curl -sS "${HDRS[@]}" "$BASE/policySets/gboc-smoke-ps"
curl -sS "${HDRS[@]}" "$BASE/fleets/gboc-smoke-fleet-windows"
curl -sS "${HDRS[@]}" "$BASE/policySets/gboc-smoke-ps-windows"
```

## Running locally

The test requires `.deb`/`.rpm` (Linux) and `.goo` (Windows) collector packages
in GCS built with the `googlecontrolplane` provider, extension, and
`googlepolicy` processor enabled.

### On-GCP mode (`stackdriver-test-143416`, Linux + Windows)

```
cd integration_test/smoke_test
env PROJECT=stackdriver-test-143416 ZONES=asia-east1-a \
  IMAGE_SPECS=debian-cloud:debian-12,windows-cloud:windows-2022 \
  KOKORO_BUILD_ID=local-$(date +%s) TRANSFERS_BUCKET=stackdriver-test-143416-file-transfers \
  _BUILD_ARTIFACTS_PACKAGE_GCS=gs://stackdriver-test-143416-file-transfers/<package-dir> \
  TELEMETRY_POLICY_CONTROL_PLANE_ENDPOINT=staging-telemetrydirector.asia-east1.rep.sandbox.googleapis.com:443 \
  TELEMETRY_POLICY_EXPECTED_REVISION=68668fd3 \
  TELEMETRY_POLICY_EXPECTED_REVISION_WINDOWS=86f9ce08 \
  go test -tags integration_test -run '^TestTelemetryPoliciesSmoke$' -v -timeout 60m ./...
```

### Off-GCP mode (`<OFF_GCP_PROJECT_ID>`, Linux + Windows)

With `OFF_GCP=true`, the test creates the GCE VM with the same helper as
on-GCP, installs the collector package from GCS, and writes helper scripts
([`ensure_firewall_linux.sh`](ensure_firewall_linux.sh),
[`block_metadata_linux.sh`](block_metadata_linux.sh),
[`configure_windows.ps1`](configure_windows.ps1)), filtered `hosts`, and the
Secret Manager X.509 WIF credentials directly onto the VM over SSH. It then
stops the VM, detaches its service account and OAuth scopes
(`gce.DetachServiceAccount`), and starts it back up:

- **Linux:** The systemd drop-in runs `block_metadata.sh` (`ExecStartPre=+`) to
  reject non-DNS TCP traffic to `169.254.169.254` from `otelcol-google`
  (preserving port 53 DNS and `root`'s `google-guest-agent` SSH), bind-mounts a
  filtered `/etc/hosts` mapping `metadata.google.internal` to `127.0.0.1`, sets
  `GOOGLE_APPLICATION_CREDENTIALS` (for X.509 WIF) and `GOOGLE_CLOUD_PROJECT`
  (required by `googleclientauth/default_gcp_destination` for `external_account`
  credentials), and passes `project`, `project_number`, and `region` on the
  `xds://` URI.
- **Windows:** `configure_windows.ps1` adds a Windows Defender Firewall outbound
  block rule for `169.254.169.254` scoped to `otelcol-google.exe`, updates
  `C:\Windows\System32\drivers\etc\hosts` (and flushes DNS cache) so
  `metadata.google.internal` resolves to `127.0.0.1`, sets
  `GOOGLE_APPLICATION_CREDENTIALS` and `GOOGLE_CLOUD_PROJECT` on the
  `otelcol-google` Windows service, and passes `project`, `project_number`, and
  `region` on the `xds://` URI.

```
cd integration_test/smoke_test
env PROJECT=<OFF_GCP_PROJECT_ID> ZONES=asia-east1-a \
  IMAGE_SPECS=debian-cloud:debian-12,windows-cloud:windows-2022 \
  KOKORO_BUILD_ID=local-$(date +%s) TRANSFERS_BUCKET=<OFF_GCP_TRANSFERS_BUCKET> \
  _BUILD_ARTIFACTS_PACKAGE_GCS=gs://<OFF_GCP_TRANSFERS_BUCKET>/<package-dir> \
  TELEMETRY_POLICY_CONTROL_PLANE_ENDPOINT=staging-telemetrydirector.asia-east1.rep.sandbox.googleapis.com:443 \
  TELEMETRY_POLICY_EXPECTED_REVISION=1b20a331 \
  TELEMETRY_POLICY_EXPECTED_REVISION_WINDOWS=3d90357c \
  OFF_GCP=true PROJECT_NUMBER=406580743452 \
  go test -tags integration_test -run '^TestTelemetryPoliciesSmoke$' -v -timeout 60m ./...
```
