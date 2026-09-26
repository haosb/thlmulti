// Command thlmulti is a terminal multiplexer with tabs, and nothing else.
// How it works is in internal/app.
package main

import (
	"fmt"
	"os"

	"github.com/haosb/thlmulti/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "thlmulti:", err)
		os.Exit(1)
	}
}
