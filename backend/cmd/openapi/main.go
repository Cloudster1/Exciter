// Command openapi writes the API's OpenAPI document, derived from the server's
// own route table and types, to the path given or to stdout.
package main

import (
	"fmt"
	"os"

	"github.com/cloudster1/exciter/backend/internal/httpapi"
)

func main() {
	encoded, err := httpapi.Spec().MarshalIndent()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoded = append(encoded, '\n')
	if len(os.Args) > 1 {
		if err := os.WriteFile(os.Args[1], encoded, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	os.Stdout.Write(encoded)
}
