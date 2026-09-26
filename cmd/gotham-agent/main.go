// Command gotham-agent is the Gotham node agent entrypoint.
package main

import "fmt"

// version is the reported build version. Released binaries override it with
// -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	// The real agent lands in Phase 2.
	fmt.Printf("gotham-agent %s\n", version)
}
