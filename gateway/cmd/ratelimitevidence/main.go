// Command ratelimitevidence writes and verifies the one canonical production
// record proving the auth rate limits passed at the checked out revision.
package main

import (
	"errors"
	"fmt"
	"os"
)

var errUsage = errors.New("usage: ratelimitevidence <write|verify> <repository-root> <evidence-path>")

const argumentCount = 3

func main() {
	err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "ratelimitevidence: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != argumentCount {
		return errUsage
	}
	switch args[0] {
	case "write":
		return writeEvidence(args[1], args[2], nil)
	case "verify":
		return verifyEvidence(args[1], args[2])
	default:
		return errUsage
	}
}
