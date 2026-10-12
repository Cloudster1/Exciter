// Command docallowlist writes the document allowlist as JSON to the path given, or to stdout, for the web
// editor's test to hold its schema to.
package main

import (
	"fmt"
	"os"

	"github.com/cloudster1/exciter/backend/internal/document"
)

func main() {
	encoded, err := document.Allowed.JSON()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(os.Args) > 1 {
		if err := os.WriteFile(os.Args[1], encoded, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	os.Stdout.Write(encoded)
}
