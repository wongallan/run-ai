package main

import (
	"os"

	"run-ai/internal/cli"
)

func main() {
	baseDir, err := os.Getwd()
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to resolve working directory\n")
		os.Exit(1)
	}

	exitCode := cli.Run(os.Args[1:], os.Stdout, os.Stderr, baseDir)
	os.Exit(exitCode)
}
