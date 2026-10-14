// Command docallowlist writes the document allowlist as JSON to the path given, or to stdout, and the comment
// allowlist to a second path, for the web editors' tests to hold their schemas to.
package main

import (
	"fmt"
	"os"

	"github.com/cloudster1/exciter/backend/internal/document"
)

// fileMode is what a generated file under api/ is checked in with.
const fileMode = 0o644

func main() {
	encoded, err := document.Allowed.JSON()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(os.Args) > 1 {
		if err := os.WriteFile(os.Args[1], encoded, fileMode); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if len(os.Args) > 2 {
		comment, err := document.CommentAllowed.JSON()
		if err == nil {
			err = os.WriteFile(os.Args[2], comment, fileMode)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if len(os.Args) > 1 {
		return
	}
	os.Stdout.Write(encoded)
}
