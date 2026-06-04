// Package charts embeds the operator Helm chart so the CLI can render and apply
// it in-memory without a checked-in copy. The chart lives in place at
// charts/yacd; this package sits one directory up so //go:embed — which cannot
// traverse ".." — can still reach it. controller-gen writes CRDs into
// charts/yacd/crds and they are embedded directly, so there is no second copy to
// keep in sync.
package charts

import (
	"embed"
	"io/fs"
)

// The all: prefix is mandatory: without it embed silently drops _helpers.tpl
// (and any file whose name begins with "_" or "."), and the render fails on the
// missing named templates.
//
//go:embed all:yacd
var embedded embed.FS

// OperatorChart is the operator Helm chart filesystem, rooted at the chart
// directory: its top-level entries are Chart.yaml, values.yaml, templates/, and
// crds/. It is the single source of truth the CLI renders and applies.
var OperatorChart = mustSub(embedded, "yacd")

// mustSub roots embedded at the chart subdirectory. The error is unreachable:
// dir is a static subdirectory embedded into the binary at build time.
func mustSub(f embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic("charts: sub filesystem " + dir + ": " + err.Error())
	}
	return sub
}
