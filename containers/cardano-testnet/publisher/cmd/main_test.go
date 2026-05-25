package main

import (
	"os"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// TestMain registers the publisher binary so testscript cases can
// invoke it by name. The registered entrypoint wraps run() so exit
// codes flow through testscript.Main, which handles m.Run and
// os.Exit internally.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"yacd-cardano-testnet-publisher": func() { os.Exit(run()) },
	})
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
