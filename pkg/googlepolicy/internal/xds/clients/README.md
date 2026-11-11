# Vendored `google.golang.org/grpc/internal/xds/clients`

This directory contains a vendored copy of the generic gRPC-Go xDS client from
`google.golang.org/grpc@v1.84.0` (`internal/xds/clients`).

## Why Is This Vendored?

Upstream is stabilizing and exporting `google.golang.org/grpc/internal/xds/clients`
as a public package (`google.golang.org/grpc/xds/clients`), tracked in
[grpc/grpc-go#9455](https://github.com/grpc/grpc-go/issues/9455). Once the upstream
package is publicly exported and `pkg/googlepolicy/go.mod` is updated to a `grpc-go`
release containing it, this directory should be removed (`rm -rf pkg/googlepolicy/internal/xds`)
and callers in `pkg/googlepolicy` should import `google.golang.org/grpc/xds/clients/...`
directly.

## Upstream Source & Scope

- **Upstream module version:** `google.golang.org/grpc@v1.84.0`
- **Copied trees:**
  - `internal/xds/clients/...` -> `pkg/googlepolicy/internal/xds/clients/...`
  - `internal/grpclog/prefix_logger.go` -> `pkg/googlepolicy/internal/xds/clients/internal/grpclog/prefix_logger.go`
  - `internal/grpctest/grpctest.go` -> `pkg/googlepolicy/internal/xds/clients/internal/grpctest/grpctest.go`
- **Omitted subpackages and files** (not needed by ADS client or dependent on internal `grpc-go` e2e test harnesses):
  - `lrsclient/`
  - `xdsclient/test/`
  - `internal/testutils/e2e/`
  - `grpctransport/grpc_transport_ext_test.go`

## Mechanical Decoupling Edits

To compile outside the `google.golang.org/grpc` module, only the following mechanical
adjustments were applied during the initial vendor copy:

1. **Internal import paths rewritten** across all `.go` files:
   - `google.golang.org/grpc/internal/xds/clients` -> `github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/xds/clients`
   - `google.golang.org/grpc/internal/grpclog` -> `github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/xds/clients/internal/grpclog`
   - `google.golang.org/grpc/internal/grpctest` -> `github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/xds/clients/internal/grpctest`
2. **`xdsclient/channel.go`:** Replaced `envconfig.XDSRecoverPanicInResourceParsing`
   with `true` (its upstream default) and removed the `google.golang.org/grpc/internal/envconfig` import.
3. **`internal/grpctest/grpctest.go`:** Stripped `google.golang.org/grpc/internal/leakcheck`
   and `tLogr` calls from `Tester.Setup` and `Tester.Teardown`.
4. **`xdsclient/channel_test.go`:** Removed the 4 test functions (`TestChannel_ADS_HandleResponseWatchExpiry`,
   `TestChannel_ADS_StreamFailure`, `TestChannel_ADS_ResourceUnsubscribe`, and
   `TestDecodeResponse_PanicRecoveryDisabled`) that depend on `internal/testutils/e2e`
   and `google.golang.org/grpc/internal/envconfig`.

## Exact Reproduction Script

From the repository root (with `google.golang.org/grpc v1.84.0` in `pkg/googlepolicy/go.mod`):

```bash
GRPC_SRC="$(go list -C pkg/googlepolicy -m -f '{{.Dir}}' google.golang.org/grpc)"
DEST="pkg/googlepolicy/internal/xds/clients"

rm -rf "$DEST"
mkdir -p "$DEST"
cp -R "$GRPC_SRC/internal/xds/clients/." "$DEST/"
chmod -R u+w "$DEST"
mkdir -p "$DEST/internal/grpclog" "$DEST/internal/grpctest"
cp "$GRPC_SRC/internal/grpclog/prefix_logger.go" "$DEST/internal/grpclog/"
cp "$GRPC_SRC/internal/grpctest/grpctest.go" "$DEST/internal/grpctest/"
chmod -R u+w "$DEST"

rm -rf \
  "$DEST/lrsclient" \
  "$DEST/xdsclient/test" \
  "$DEST/internal/testutils/e2e" \
  "$DEST/grpctransport/grpc_transport_ext_test.go"

find "$DEST" -name '*.go' -exec sed -i \
  -e 's|"google.golang.org/grpc/internal/xds/clients|"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/xds/clients|g' \
  -e 's|"google.golang.org/grpc/internal/grpclog"|"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/xds/clients/internal/grpclog"|g' \
  -e 's|"google.golang.org/grpc/internal/grpctest"|"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/xds/clients/internal/grpctest"|g' \
  {} +

sed -i \
  -e '/"google.golang.org\/grpc\/internal\/envconfig"/d' \
  -e 's/envconfig\.XDSRecoverPanicInResourceParsing/true/g' \
  "$DEST/xdsclient/channel.go"
sed -i -e '33,46d' -e '56,63d' -e '68,83d' "$DEST/internal/grpctest/grpctest.go"
sed -i -e '468,661d' -e '802,821d' "$DEST/xdsclient/channel_test.go"
.tools/goimports -w "$DEST/internal/grpctest/grpctest.go" "$DEST/xdsclient/channel_test.go"

GOWORK=off go -C pkg/googlepolicy mod tidy
GOWORK=off go test -C pkg/googlepolicy ./internal/xds/clients/...
```
