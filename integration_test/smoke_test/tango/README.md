# Tango-GBOC smoke test setup

`TestTangoSmoke` (`../tango_smoke_test.go`) runs `otelcol-google` on a GCE VM,
configured only through
`--config=googlecontrolplane:xds://$TANGO_CP_ENDPOINT?gcp.fleet_id=$TANGO_FLEET_ID&region=$TANGO_REGION`,
and verifies that:

1. the collector connects to the Control Plane, receives the pre-provisioned
   PolicySet revision and applies (ACKs) it;
2. the self-o11y gauge `prometheus.googleapis.com/gcp.policy.set.active/gauge`
   is exported to the Data Plane and, observed through Cloud Monitoring,
   reports the same PolicySet (`gcp.policy.set.id`) and revision.

The test assumes a long-lived fleet and PolicySet already exist and have been
rolled out. It never creates, updates or deletes them.

Current resources (project `lujieduan-sandbox`, number `648321971081`,
location `asia-east1`, staging Telemetry Management API, staging Telemetry
Director `staging-telemetrydirector.asia-east1.rep.sandbox.googleapis.com:443`):

| Resource | Name |
|---|---|
| Fleet | `projects/lujieduan-sandbox/locations/asia-east1/fleets/gboc-smoke-fleet` |
| PolicySet | `projects/648321971081/locations/asia-east1/policySets/gboc-smoke-ps` |
| Revision | `projects/648321971081/locations/asia-east1/policySets/gboc-smoke-ps/revisions/3f038775` |

The PolicySet body is in [`policyset.json`](policyset.json). It contains a
journald source policy, a log transform policy and two log filter policies.
The Control Plane currently translates only `universalPolicies[].logFilterPolicy`
into the xDS `TelemetryCollector` resource, so the collector reports
`policies_loaded: 2`. The test only requires a non-empty PolicySet.

## Recreating the resources

[`setup_policyset.sh`](setup_policyset.sh) performs the one-time project opt-in
and creates (or updates) the fleet and PolicySet, then rolls the latest revision
out to the fleet. It is not run by CI.

```
./setup_policyset.sh lujieduan-sandbox
```

A newly created fleet can take several minutes to become visible to Telemetry
Director. Until then the collector logs
`NotFound desc = Traffic Director configuration was not found`.

## Inspecting the resources

```
P=lujieduan-sandbox
BASE=https://staging-telemetrymanagement.sandbox.googleapis.com/v1alpha/projects/$P/locations/asia-east1
HDRS=(-H "Authorization: Bearer $(gcloud auth print-access-token)" -H "X-Goog-User-Project: $P" -H "X-Goog-Visibilities: TRUSTED_TESTER_COLLECTORS")

curl -sS "${HDRS[@]}" "$BASE/fleets/gboc-smoke-fleet"
curl -sS "${HDRS[@]}" "$BASE/policySets/gboc-smoke-ps"
curl -sS "${HDRS[@]}" "$BASE/policySets/gboc-smoke-ps/revisions/3f038775"
curl -sS "${HDRS[@]}" "$BASE/fleets/gboc-smoke-fleet/rollouts"
```

The `X-Goog-Visibilities` header is required. Without it the API answers
`404 Method not visible`.

## Running locally

The test needs a `.deb`/`.rpm` package of the collector in GCS. Use a build
that predates #768: until the Control Plane's copy of the policy protos is
updated, newer collectors NACK the filter policies it sends.

```
cd integration_test/smoke_test
env PROJECT=lujieduan-sandbox ZONES=asia-east1-a IMAGE_SPECS=debian-cloud:debian-12 \
  KOKORO_BUILD_ID=local-$(date +%s) TRANSFERS_BUCKET=<bucket> \
  _BUILD_ARTIFACTS_PACKAGE_GCS=gs://<bucket>/<package-dir> \
  TANGO_CP_ENDPOINT=staging-telemetrydirector.asia-east1.rep.sandbox.googleapis.com:443 \
  TANGO_EXPECTED_REVISION=3f038775 \
  go test -tags integration_test -run TestTangoSmoke -v -timeout 60m ./...
```
