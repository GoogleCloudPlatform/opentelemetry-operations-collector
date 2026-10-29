# Tango-GBOC smoke test setup

`TestTangoSmoke` (`../tango_smoke_test.go`) runs `otelcol-google` on a GCE VM,
configured only through
`--config=googlecontrolplane:xds://$TANGO_CP_ENDPOINT?gcp.fleet_id=$TANGO_FLEET_ID`.
It assumes a long-lived fleet and PolicySet already exist in the test project
and have been rolled out. The test never creates, updates or deletes them.

Current resources (project `stackdriver-test-143416`, number `228437877067`,
location `asia-east1`, staging Telemetry Management API):

| Resource | Name |
|---|---|
| Fleet | `projects/stackdriver-test-143416/locations/asia-east1/fleets/gboc-smoke-fleet` |
| PolicySet | `projects/228437877067/locations/asia-east1/policySets/gboc-smoke-ps` |
| Revision | `projects/228437877067/locations/asia-east1/policySets/gboc-smoke-ps/revisions/904e9adb` |

The PolicySet body is in [`policyset.json`](policyset.json):

- `journaldSourcePolicy`: collects the system journal.
- `logTransformPolicy`: adds attribute `gboc_smoke="true"` to records whose
  body `SYSLOG_IDENTIFIER` is `gboc-smoke`.
- `logFilterPolicy` (`ACTION_DROP`): drops records whose body `MESSAGE` contains
  `gboc-smoke=drop`.
- `logFilterPolicy` (`ACTION_DROP`): drops every record whose `SYSLOG_IDENTIFIER`
  is not `gboc-smoke`, so only the test probes are exported.

## Recreating the resources

[`setup_policyset.sh`](setup_policyset.sh) performs the one-time project opt-in
and creates (or updates) the fleet and PolicySet, then rolls the latest revision
out to the fleet. It is not run by CI.

```
./setup_policyset.sh stackdriver-test-143416
```

## Inspecting the resources

```
P=stackdriver-test-143416
BASE=https://staging-telemetrymanagement.sandbox.googleapis.com/v1alpha/projects/$P/locations/asia-east1
HDRS=(-H "Authorization: Bearer $(gcloud auth print-access-token)" -H "X-Goog-User-Project: $P" -H "X-Goog-Visibilities: TRUSTED_TESTER_COLLECTORS")

curl -sS "${HDRS[@]}" "$BASE/fleets/gboc-smoke-fleet"
curl -sS "${HDRS[@]}" "$BASE/policySets/gboc-smoke-ps"
curl -sS "${HDRS[@]}" "$BASE/policySets/gboc-smoke-ps/revisions/904e9adb"
curl -sS "${HDRS[@]}" "$BASE/fleets/gboc-smoke-fleet/rollouts"
```

The `X-Goog-Visibilities` header is required. Without it the API answers
`404 Method not visible`.
