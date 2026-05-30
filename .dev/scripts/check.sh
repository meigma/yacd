#!/usr/bin/env bash
set -euo pipefail

echo "== go format =="
go_roots=(cli cmd containers/cardano-testnet containers/cardano-tools services test)
for optional_dir in api internal; do
  if [ -d "$optional_dir" ]; then
    go_roots+=("$optional_dir")
  fi
done
unformatted="$(find "${go_roots[@]}" -name '*.go' -type f -print0 | xargs -0 gofmt -l)"
if [ -n "$unformatted" ]; then
  echo "Go files need formatting:" >&2
  printf '%s\n' "$unformatted" >&2
  exit 1
fi

echo "== go lint =="
golangci-lint config verify --config .golangci.yml
golangci-lint run --config .golangci.yml ./... --show-stats=false
(cd containers/cardano-testnet/publisher && golangci-lint run --config ../../../.golangci.yml ./... --show-stats=false)

echo "== cardano-testnet tools tests =="
(cd containers/cardano-testnet && go test ./...)
(cd containers/cardano-testnet/publisher && go test ./...)

echo "== generated artifacts =="
controller-gen object paths="./..."
go test ./test/chart -run TestManagerRBACMatchesControllerGen -count=1
git diff --exit-code -- api charts/yacd/crds

echo "== helm chart =="
chart="charts/yacd"
package_dir="$(mktemp -d)"
trap 'rm -rf "$package_dir"' EXIT
python3 -m json.tool "$chart/values.schema.json" >/dev/null
helm lint "$chart"
helm template yacd "$chart" --namespace yacd-system --include-crds >/dev/null
helm install yacd "$chart" --namespace yacd-system --dry-run=client --server-side=false >/dev/null
helm package "$chart" --destination "$package_dir" >/dev/null

echo "== chainsaw manifests =="
chainsaw lint configuration --file test/chainsaw/chainsaw-config.yaml
find test/chainsaw -name chainsaw-test.yaml -print | while IFS= read -r test_file; do
  chainsaw lint test --file "$test_file"
done
