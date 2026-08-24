package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/sibstark/athena/fetcher/internal/bitrix"
	"github.com/sibstark/athena/fetcher/internal/config"
	"github.com/sibstark/athena/fetcher/internal/normalizer"
	"github.com/sibstark/athena/fetcher/internal/snapshot"
)

var version = "0.1.0"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stderr)
		return errors.New("a command is required")
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, version)
		return nil
	case "auth":
		if len(args) != 2 || args[1] != "check" {
			return errors.New("usage: fetcher auth check")
		}
		client, err := createClient()
		if err != nil {
			return err
		}
		workspaces, err := client.ListWorkspaces(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Connected to %s. Accessible workspaces: %d.\n", client.PortalOrigin(), len(workspaces))
		return nil
	case "workspaces":
		if len(args) != 2 || args[1] != "list" {
			return errors.New("usage: fetcher workspaces list")
		}
		client, err := createClient()
		if err != nil {
			return err
		}
		workspaces, err := client.ListWorkspaces(ctx)
		if err != nil {
			return err
		}
		writer := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(writer, "ID\tNAME\tACCESS\tUPDATED AT")
		for _, workspace := range workspaces {
			fmt.Fprintf(writer, "%d\t%s\t%s\t%s\n", workspace.ID, workspace.Name, workspace.PolicyLevel, workspace.UpdatedAt)
		}
		return writer.Flush()
	case "fetch":
		return runFetch(ctx, args[1:], stdout, stderr)
	case "normalize":
		return runNormalize(args[1:], stdout, stderr)
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runNormalize(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("normalize", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var runDirectory string
	var output string
	var include idSet
	var exclude idSet
	var shortNoteThreshold int
	var force bool
	flags.StringVar(&runDirectory, "run", "", "finalized fetcher run directory (required)")
	flags.StringVar(&output, "out", "", "separate output directory for normalized data (required)")
	flags.Var(&include, "include", "workspace IDs to include; comma-separated or repeated")
	flags.Var(&exclude, "exclude", "workspace IDs to exclude; comma-separated or repeated")
	flags.IntVar(&shortNoteThreshold, "short-note-threshold", 200, "short note threshold in Unicode characters")
	flags.BoolVar(&force, "force", false, "atomically replace an existing output directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	result, err := normalizer.Normalize(normalizer.Options{
		RunDir: runDirectory, OutputDir: output, Include: include.values, Exclude: exclude.values,
		ShortNoteThreshold: shortNoteThreshold, Force: force,
		Progress: func(message string) { fmt.Fprintln(stdout, message) },
	})
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "Status: %s\n", result.Status)
	fmt.Fprintf(stdout, "Workspaces: %d\n", result.WorkspaceCount)
	fmt.Fprintf(stdout, "Documents: %d normalized, %d suspicious\n", result.ProcessedDocuments, result.SuspiciousDocuments)
	fmt.Fprintf(stdout, "Diagnostics: %d errors, %d warnings\n", result.Errors, result.Warnings)
	fmt.Fprintf(stdout, "Output: %s\n", result.OutputDir)
	return nil
}

func runFetch(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("fetch", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var output string
	var include idSet
	var exclude idSet
	var skipAssets bool
	flags.StringVar(&output, "out", "", "archive root for immutable run directories (required)")
	flags.Var(&include, "include", "workspace IDs to include; comma-separated or repeated")
	flags.Var(&exclude, "exclude", "workspace IDs to exclude; comma-separated or repeated")
	flags.BoolVar(&skipAssets, "skip-assets", false, "keep remote asset URLs instead of downloading files")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	client, err := createClient()
	if err != nil {
		return err
	}
	exporter := snapshot.NewExporter(client)
	summary, err := exporter.Fetch(ctx, snapshot.Options{
		OutputDir: output, Include: include.values, Exclude: exclude.values, SkipAssets: skipAssets,
		Progress: func(message string) { fmt.Fprintln(stdout, message) },
	})
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "Run: %s\n", summary.RunID)
	fmt.Fprintf(stdout, "Status: %s\n", summary.Status)
	fmt.Fprintf(stdout, "Workspaces: %d\n", summary.WorkspaceCount)
	fmt.Fprintf(stdout, "Documents: %d\n", summary.DocumentCount)
	fmt.Fprintf(stdout, "Assets: %d downloaded, %d failed\n", summary.AssetsDownloaded, summary.AssetsFailed)
	fmt.Fprintf(stdout, "Output: %s\n", summary.RunPath)
	return nil
}

func createClient() (*bitrix.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return bitrix.NewClient(cfg.WebhookURL, bitrix.ClientOptions{})
}

type idSet struct {
	values map[int]struct{}
}

func (set *idSet) String() string {
	if len(set.values) == 0 {
		return ""
	}
	items := make([]string, 0, len(set.values))
	for id := range set.values {
		items = append(items, strconv.Itoa(id))
	}
	return strings.Join(items, ",")
}

func (set *idSet) Set(value string) error {
	if set.values == nil {
		set.values = make(map[int]struct{})
	}
	for _, raw := range strings.Split(value, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return errors.New("workspace ID list contains an empty value")
		}
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			return fmt.Errorf("workspace ID must be a positive integer: %q", raw)
		}
		set.values[id] = struct{}{}
	}
	return nil
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage:")
	fmt.Fprintln(writer, "  fetcher auth check")
	fmt.Fprintln(writer, "  fetcher workspaces list")
	fmt.Fprintln(writer, "  fetcher fetch --out <directory> [--include IDs] [--exclude IDs] [--skip-assets]")
	fmt.Fprintln(writer, "  fetcher normalize --run <run_directory> --out <directory> [--include IDs] [--exclude IDs] [--short-note-threshold N] [--force]")
}
