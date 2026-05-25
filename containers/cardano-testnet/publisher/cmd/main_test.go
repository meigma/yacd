package main

import (
	"os"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// TestMain registers the publisher binary so testscript cases can
// invoke it by name. The registered entrypoint is the existing run()
// function so exit codes flow through unchanged.
func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"yacd-cardano-testnet-publisher": run,
	}))
}

// TestPublish exercises the publish pipeline end-to-end via golden
// testscript cases under testdata/. Set UPDATE_SCRIPTS=1 to refresh
// the inline golden files after an intentional output change.
func TestPublish(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir:           "testdata",
		UpdateScripts: os.Getenv("UPDATE_SCRIPTS") == "1",
	})
}
