// Command devkeys prints a fresh Ed25519 signing key pair as the two
// environment lines the system needs: the private half for identity, which
// alone mints tokens, and the public half for everybody who verifies them
// (STK-14, INV-14).
//
// Development only. Feature 5 supplies the real pair as a Kubernetes Secret,
// and the same two variable names are what it sets, so no code changes.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
)

func main() {
	kid := "dev-1"
	if len(os.Args) > 1 && os.Args[1] != "" {
		kid = os.Args[1]
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintln(os.Stderr, "devkeys:", err)
		os.Exit(1)
	}
	fmt.Printf("IDENTITY_TOKEN_KID=%s\n", kid)
	fmt.Printf("IDENTITY_TOKEN_PRIVATE_KEY=%s\n", base64.StdEncoding.EncodeToString(private))
	fmt.Printf("TOKEN_PUBLIC_KEYS=%s:%s\n", kid, base64.StdEncoding.EncodeToString(public))
}
