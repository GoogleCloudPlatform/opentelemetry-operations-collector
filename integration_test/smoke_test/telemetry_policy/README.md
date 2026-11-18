# Telemetry Policy Smoke Test

`TestTelemetryPoliciesSmoke` ([`telemetry_policy_smoke_test.go`](../telemetry_policy_smoke_test.go)) installs `otelcol-google` on a Linux or Windows GCE VM, runs it solely with `--config=googlecontrolplane:xds://...` (no local `config.yaml`), waits for `prometheus.googleapis.com/gcp.policy.set.active/gauge` to report the expected PolicySet revision, writes [`sample_logs.log`](sample_logs.log), and verifies in Cloud Logging that `gboc-smoke=keep` arrives with `labels.gboc_smoke="true"` while `gboc-smoke=drop` and `smoke-test=other` are dropped.

Linux (`gboc-smoke-fleet` / `gboc-smoke-ps`, [`policyset.json`](policyset.json)) and Windows (`gboc-smoke-fleet-windows` / `gboc-smoke-ps-windows`, [`policyset_windows.json`](policyset_windows.json)) use separate fleets and PolicySets. Both modes share the same PolicySet definitions:

- **On-GCP:** Runs in a Google-org project (`<google-org-project-name>`, `<google-org-project-number>`) using the VM's default Compute Engine service account and metadata server.
- **Off-GCP (`OFF_GCP=true`):** Runs in a non-Google-org project (`<joonix-org-project-name>`, `<joonix-org-project-number>`) where external X.509 Workload Identity Federation (WIF) pools are permitted, boots the VM without a service account or scopes (`--no-service-account --no-scopes`), blocks the metadata server for `otelcol-google`, and authenticates with X.509 WIF credentials fetched from Secret Manager (or `WIF_DIR`).

## Provisioning

Fleets, PolicySets, and WIF resources must already exist before running the test. [`setup_policyset.sh`](setup_policyset.sh) creates or updates them idempotently (not run by CI):

```bash
# On-GCP fleets and PolicySets only
./setup_policyset.sh <google-org-project-name>

# Off-GCP fleets, PolicySets, and X.509 WIF pool/provider/secrets
./setup_policyset.sh <joonix-org-project-name> asia-east1 <joonix-org-project-name>
```

When a PolicySet changes and produces a new revision, update `TELEMETRY_POLICY_EXPECTED_REVISION` / `TELEMETRY_POLICY_EXPECTED_REVISION_WINDOWS` accordingly.

## Running locally

Requires built `.deb`/`.rpm` and `.goo` packages in `_BUILD_ARTIFACTS_PACKAGE_GCS` with `googlecontrolplane` and `googlepolicy` enabled. Off-GCP runs also require `roles/secretmanager.secretAccessor` on the WIF leaf secrets (or local `leaf.pem`/`leaf.key` via `WIF_DIR`).

**On-GCP (`<google-org-project-name>`):**

```bash
cd integration_test/smoke_test
env PROJECT=<google-org-project-name> ZONES=asia-east1-a \
  IMAGE_SPECS=debian-cloud:debian-12,windows-cloud:windows-2022 \
  KOKORO_BUILD_ID=local-$(date +%s) TRANSFERS_BUCKET=<google-org-transfers-bucket> \
  _BUILD_ARTIFACTS_PACKAGE_GCS=gs://<google-org-transfers-bucket>/<package-dir> \
  TELEMETRY_POLICY_CONTROL_PLANE_ENDPOINT=staging-telemetrydirector.asia-east1.rep.sandbox.googleapis.com:443 \
  TELEMETRY_POLICY_EXPECTED_REVISION=<linux-rev> \
  TELEMETRY_POLICY_EXPECTED_REVISION_WINDOWS=<windows-rev> \
  go test -tags integration_test -run '^TestTelemetryPoliciesSmoke$' -v -timeout 60m ./...
```

If `<google-org-project-name>` fleets are unavailable, override `FLEET_ID=projects/<joonix-org-project-name>/locations/asia-east1/fleets/gboc-smoke-fleet` and `WINDOWS_FLEET_ID=projects/<joonix-org-project-name>/locations/asia-east1/fleets/gboc-smoke-fleet-windows` (with the `<joonix-org-project-name>` revisions).

**Off-GCP (`<joonix-org-project-name>`):**

```bash
cd integration_test/smoke_test
env PROJECT=<joonix-org-project-name> ZONES=asia-east1-a \
  IMAGE_SPECS=debian-cloud:debian-12,windows-cloud:windows-2022 \
  KOKORO_BUILD_ID=local-$(date +%s) TRANSFERS_BUCKET=<joonix-org-transfers-bucket> \
  _BUILD_ARTIFACTS_PACKAGE_GCS=gs://<joonix-org-transfers-bucket>/<package-dir> \
  TELEMETRY_POLICY_CONTROL_PLANE_ENDPOINT=staging-telemetrydirector.asia-east1.rep.sandbox.googleapis.com:443 \
  TELEMETRY_POLICY_EXPECTED_REVISION=<linux-rev> \
  TELEMETRY_POLICY_EXPECTED_REVISION_WINDOWS=<windows-rev> \
  OFF_GCP=true PROJECT_NUMBER=<joonix-org-project-number> \
  go test -tags integration_test -run '^TestTelemetryPoliciesSmoke$' -v -timeout 60m ./...
```

See [`telemetry_policy_smoke_test.go`](../telemetry_policy_smoke_test.go) for all supported environment variables.
