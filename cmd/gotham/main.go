// Command gotham is the Gotham control-plane entrypoint.
package main

import "fmt"

// version is the reported build version. Released binaries override it with
// -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	// The real `serve` command is implemented in BE-0.2.
	fmt.Printf("gotham %s\n", version)
}
