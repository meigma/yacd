package main

import (
	"context"
	"fmt"
	"os"

	"github.com/meigma/yacd/containers/cardano-testnet/internal/artifactpublisher"
)

func main() {
	if err := artifactpublisher.Run(context.Background(), os.Args[1:], artifactpublisher.Environ(), os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "yacd-cardano-testnet-publisher: %v\n", err)
		os.Exit(1)
	}
}
