#!/usr/bin/env bash
set -euo pipefail

echo "== go format =="
unformatted="$(find api cmd internal test -name '*.go' -type f -print0 | xargs -0 gofmt -l)"
if [ -n "$unformatted" ]; then
  echo "Go files need formatting:" >&2
  printf '%s\n' "$unformatted" >&2
  exit 1
fi

echo "== go lint =="
golangci-lint config verify --config .golangci.yml
golangci-lint run --config .golangci.yml ./... --show-stats=false

echo "== generated artifacts =="
controller-gen object paths="./..."
controller-gen crd paths="./..." output:crd:artifacts:config=charts/template-k8s/crds
go test ./test/chart -run TestManagerRBACMatchesControllerGen -count=1
git diff --exit-code -- api charts/template-k8s/crds

echo "== helm chart =="
chart="charts/template-k8s"
package_dir="$(mktemp -d)"
trap 'rm -rf "$package_dir"' EXIT
python3 -m json.tool "$chart/values.schema.json" >/dev/null
helm lint "$chart"
helm template template-k8s "$chart" --namespace template-k8s-system --include-crds >/dev/null
helm install template-k8s "$chart" --namespace template-k8s-system --dry-run=client --server-side=false >/dev/null
helm package "$chart" --destination "$package_dir" >/dev/null

echo "== chainsaw manifests =="
chainsaw lint configuration --file test/chainsaw/chainsaw-config.yaml
find test/chainsaw -name chainsaw-test.yaml -print | while IFS= read -r test_file; do
  chainsaw lint test --file "$test_file"
done
