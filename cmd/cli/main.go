package main

import (
	"fmt"
	"os"
)

func main() {
	opts, args, err := parseGlobalFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if len(args) == 0 || isHelpArg(args[0]) {
		printRootUsage()
		return
	}

	cfg, err := LoadConfig(opts.ConfigPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	app := &App{
		Config:     cfg,
		ConfigPath: opts.ConfigPath,
		Output:     opts.Output,
		Quiet:      opts.Quiet,
		Verbose:    opts.Verbose,
	}

	switch args[0] {
	case "version":
		cmdVersion(app, args[1:])
	case "init":
		cmdInit(app, args[1:])
	case "config":
		cmdConfig(app, args[1:])
	case "auth":
		cmdAuth(app, args[1:])
	case "manga":
		cmdManga(app, args[1:])
	case "library":
		cmdLibrary(app, args[1:])
	case "progress":
		cmdProgress(app, args[1:])
	case "sync":
		cmdSync(app, args[1:])
	case "notify":
		cmdNotify(app, args[1:])
	case "grpc":
		cmdGRPC(app, args[1:])
	case "chat":
		cmdChat(app, args[1:])
	case "server":
		cmdServer(app, args[1:])
	case "help":
		printRootUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
		printRootUsage()
		os.Exit(1)
	}
}

func cmdVersion(app *App, _ []string) {
	out := map[string]string{
		"version": Version,
		"go":      GoVersion,
	}
	printJSONOrText(app, out, "MangaHub CLI", func() {
		fmt.Printf("MangaHub CLI %s (go %s)\n", Version, GoVersion)
	})
}

func isHelpArg(value string) bool {
	switch value {
	case "-h", "--help", "help":
		return true
	default:
		return false
	}
}
