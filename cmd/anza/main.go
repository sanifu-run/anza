package main

import (
	"context"
	"io"
	"os"

	"github.com/sanifu-run/anza/internal/cli"
	"golang.org/x/term"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() { os.Exit(runContext(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

// run remains the small subprocess seam used by tests and embedders.
func run(args []string, stdout, stderr io.Writer) int {
	return runContext(context.Background(), args, os.Stdin, stdout, stderr)
}

func runContext(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	app := cli.NewApp(stdin, stdout, stderr, version)
	app.Commit, app.BuildTime = commit, buildTime
	app.Interactive = isTerminal(stdin) && isTerminal(stdout)
	return app.Run(ctx, args)
}

func isTerminal(stream any) bool {
	file, ok := stream.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}
