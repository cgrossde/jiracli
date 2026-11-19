//go:build keychainselftest

// Command selftest exercises internal/keychain against throwaway Keychain
// services. Build it only for manual signing experiments:
//
//	go build -tags keychainselftest -o <dir>/jiracli ./internal/keychain/selftest
//
// The output must be named jiracli so its code-signing identifier matches
// the release requirement.
package main

import (
	"fmt"
	"os"

	"github.com/cgrossde/jiracli/internal/keychain"
)

func main() {
	if err := keychain.SelfTest(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
