package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/service-lasso/service-lasso-pty/internal/pty"
	"github.com/service-lasso/service-lasso-pty/internal/session"
)

const version = "0.1.0"

func main() {
	flags := flag.NewFlagSet("service-lasso-pty", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	rows := flags.Uint("rows", 24, "terminal rows")
	cols := flags.Uint("cols", 80, "terminal columns")
	directory := flags.String("cwd", "", "working directory for the managed process")
	showVersion := flags.Bool("version", false, "print the helper version")
	if err := flags.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if *showVersion {
		fmt.Printf("service-lasso-pty %s\n", version)
		return
	}
	args := flags.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: service-lasso-pty [--rows N] [--cols N] [--cwd DIR] -- COMMAND [ARG...]")
		os.Exit(2)
	}
	if *rows == 0 || *rows > 1000 || *cols == 0 || *cols > 1000 {
		fmt.Fprintln(os.Stderr, "service-lasso-pty: rows and cols must be between 1 and 1000")
		os.Exit(2)
	}

	code, err := session.Run(pty.Spec{
		Command: args[0],
		Args:    args[1:],
		Dir:     *directory,
		Size:    pty.Size{Rows: uint16(*rows), Cols: uint16(*cols)},
	}, os.Stdin, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "service-lasso-pty: %v\n", err)
	}
	os.Exit(code)
}
