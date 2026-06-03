package ssa

import (
	"embed"
	"io/fs"
)

// The all: prefix is mandatory: without it embed silently drops _helpers.tpl
// (and any other file whose name begins with "_" or "."), and the render fails
// on the missing named templates.
//
//go:embed all:chart
var chartFS embed.FS

// Chart is the embedded, drift-guarded copy of charts/yacd that New renders and
// applies by default. It is exposed as an fs.FS so the composition root can pass
// it to New and tests can substitute a synthetic filesystem. The copy is kept in
// sync with the source chart by .dev/scripts/sync-operator-chart.sh (run at
// root:generate) and guarded by root:check.
var Chart fs.FS = chartFS
