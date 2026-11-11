package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

func printRootUsage() {
	fmt.Println("MangaHub CLI")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  mangahub [global flags] <command> [subcommand] [flags]")
	fmt.Println()
	fmt.Println("Global flags:")
	fmt.Println("  --config <path>   Path to config file")
	fmt.Println("  --output <fmt>    text or json")
	fmt.Println("  --verbose         Verbose output")
	fmt.Println("  --quiet           Suppress non-essential output")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  version, init, config, auth, manga, library, progress")
	fmt.Println("  sync, notify, grpc, chat, server")
	fmt.Println()
	fmt.Println("Run 'mangahub <command> --help' for command details.")
}

func printJSONOrText(app *App, data any, title string, textPrinter func()) {
	if app.Output == "json" {
		printJSON(data)
		return
	}
	if title != "" {
		fmt.Println(title)
	}
	if textPrinter != nil {
		textPrinter()
	}
}

func printJSON(data any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(data)
}

func printTable(headers []string, rows [][]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	_ = w.Flush()
}

func formatTime(ts string) string {
	if ts == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t.Format("2006-01-02 15:04:05")
	}
	return ts
}

func printKeyValues(keys []string, values map[string]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, key := range keys {
		fmt.Fprintf(w, "%s:\t%s\n", key, values[key])
	}
	_ = w.Flush()
}

func sortedKeys(m map[string][]LibraryItemView) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
