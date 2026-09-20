package main

import (
	"os"

	"openlia/operator"
)

func main() {
	os.Exit(operator.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
