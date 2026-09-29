package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("anza", flag.ContinueOnError)
	flags.SetOutput(stderr)
	showVersion := false
	showHelp := false
	flags.BoolVar(&showVersion, "version", false, "print version information")
	flags.BoolVar(&showHelp, "help", false, "show usage")
	flags.BoolVar(&showHelp, "h", false, "show usage")
	flags.Usage = func() { printUsage(stderr) }

	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	positional := flags.Args()
	if showVersion {
		if len(positional) != 0 {
			return unexpectedArguments(stderr, positional)
		}
		printVersion(stdout)
		return 0
	}
	if showHelp {
		if len(positional) != 0 {
			return unexpectedArguments(stderr, positional)
		}
		printUsage(stdout)
		return 0
	}
	if len(positional) == 1 {
		switch positional[0] {
		case "version":
			printVersion(stdout)
			return 0
		case "help":
			printUsage(stdout)
			return 0
		}
	}
	if len(positional) != 0 {
		return unexpectedArguments(stderr, positional)
	}

	printUsage(stdout)
	return 0
}

func printVersion(w io.Writer) {
	fmt.Fprintf(w, "Anza %s\nCommit: %s\nBuilt: %s\n", version, commit, buildTime)
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: anza [--version | --help]")
	fmt.Fprintln(w, "       anza version")
	fmt.Fprintln(w, "       anza help")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  help      show usage")
	fmt.Fprintln(w, "  version   print version information")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  -h, --help     show usage")
	fmt.Fprintln(w, "      --version  print version information")
}

func unexpectedArguments(stderr io.Writer, args []string) int {
	fmt.Fprintf(stderr, "anza: unexpected argument(s): %q\n", args)
	return 2
}
