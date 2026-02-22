package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"dark-factory/internal/factory"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "internal error: %v\n", r)
			os.Exit(2)
		}
	}()
	if len(os.Args) < 2 || os.Args[1] != "run" {
		fmt.Fprintln(os.Stderr, "usage: attractor run <pipeline.dot> --workdir <path> --runsdir <path> [--run-id <id>] [--resume]")
		os.Exit(1)
	}
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	workdir := fs.String("workdir", "", "source workdir")
	runsdir := fs.String("runsdir", "", "runs dir")
	runID := fs.String("run-id", "", "run id")
	resume := fs.Bool("resume", false, "resume run")
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(1)
	}
	args := fs.Args()
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "missing pipeline.dot")
		os.Exit(1)
	}
	if *workdir == "" || *runsdir == "" {
		fmt.Fprintln(os.Stderr, "--workdir and --runsdir are required")
		os.Exit(1)
	}
	if *resume && *runID == "" {
		fmt.Fprintln(os.Stderr, "--run-id required with --resume")
		os.Exit(1)
	}
	cfg := attractor.RunConfig{PipelinePath: args[0], Workdir: *workdir, Runsdir: *runsdir, RunID: *runID, Resume: *resume}
	if err := attractor.RunPipeline(cfg); err != nil {
		if errors.Is(err, os.ErrInvalid) {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
