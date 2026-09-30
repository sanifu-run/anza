package main

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/sanifu-run/anza/internal/cli"
	"github.com/sanifu-run/anza/internal/lifecycle"
	"golang.org/x/term"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
	// Populated only by the release build through -X; development builds fail
	// closed for update checks until an operator supplies a valid pinned key.
	releasePublicKeyBase64 = ""
)

func main() { os.Exit(runContext(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

// run remains the small subprocess seam used by tests and embedders.
func run(args []string, stdout, stderr io.Writer) int {
	return runContext(context.Background(), args, os.Stdin, stdout, stderr)
}

func runContext(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	app := cli.NewApp(stdin, stdout, stderr, version)
	configureReleaseUpdater(app, os.Getenv("ANZA_RELEASE_METADATA_URL"), releasePublicKeyBase64)
	app.Commit, app.BuildTime = commit, buildTime
	app.Interactive = isTerminal(stdin) && isTerminal(stdout)
	return app.Run(ctx, args)
}

func configureReleaseUpdater(app *cli.App, metadataURL, publicKeyBase64 string) {
	verifier, err := lifecycle.NewEd25519VerifierBase64(publicKeyBase64)
	if err != nil {
		app.UpdateSetupErr = err
		return
	}
	if metadataURL == "" {
		app.UpdateSetupErr = errors.New("ANZA_RELEASE_METADATA_URL is not configured")
		return
	}
	app.UpdateChecker = lifecycle.Checker{
		Source: lifecycle.HTTPReleaseSource{MetadataURL: metadataURL}, Verifier: verifier,
	}
}

func isTerminal(stream any) bool {
	file, ok := stream.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}
