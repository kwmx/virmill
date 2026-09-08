// Simulated provider fixture. Its only effects are JSON records in a private workspace.
package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	p, err := openProvider(".")
	if err == nil {
		err = p.server().Serve(context.Background(), os.Stdin, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "simulated provider refused:", err)
		os.Exit(1)
	}
}
