package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/sibstark/athena/fetcher/internal/bitrix"
	"github.com/sibstark/athena/fetcher/internal/config"
	"github.com/sibstark/athena/fetcher/internal/indexer"
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
	case "index":
		return runIndex(ctx, args[1:], stdout, stderr)
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runIndex(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if err := config.LoadEnvironment(); err != nil {
		return err
	}
	contract, err := indexer.LoadEmbeddingContract()
	if err != nil {
		return fmt.Errorf("embedding configuration: %w", err)
	}
	batchDefault, err := envPositiveInt("INDEXER_BATCH_SIZE", 16)
	if err != nil {
		return err
	}
	cacheDefault, err := envPositiveInt("EMBEDDING_VECTOR_CACHE_SIZE", 256)
	if err != nil {
		return err
	}
	qdrantTimeoutDefault, err := envDuration("QDRANT_TIMEOUT", 30*time.Second)
	if err != nil {
		return err
	}
	teiTimeoutDefault, err := envDuration("TEI_TIMEOUT", 120*time.Second)
	if err != nil {
		return err
	}
	storeEmbeddingText, err := envBool("STORE_EMBEDDING_TEXT", false)
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("index", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var runDirectory, input, collection, reportFile, qdrantURL, teiURL, logLevel string
	var qdrantTimeout, teiTimeout time.Duration
	var batchSize int
	var dryRun, forceReembed, prune, continueOnError bool
	flags.StringVar(&runDirectory, "run", "", "chunks_<id> run directory")
	flags.StringVar(&input, "input", "", "single chunks.jsonl file")
	flags.StringVar(&collection, "collection", envString("QDRANT_COLLECTION", indexer.DefaultCollection), "Qdrant collection for single-file mode")
	flags.StringVar(&reportFile, "report", "", "output report path")
	flags.StringVar(&qdrantURL, "qdrant-url", envString("QDRANT_URL", "http://127.0.0.1:6333"), "Qdrant base URL")
	flags.StringVar(&teiURL, "tei-url", envString("TEI_URL", "http://127.0.0.1:8080"), "TEI base URL")
	flags.DurationVar(&qdrantTimeout, "qdrant-timeout", qdrantTimeoutDefault, "Qdrant HTTP timeout")
	flags.DurationVar(&teiTimeout, "tei-timeout", teiTimeoutDefault, "TEI HTTP timeout")
	flags.IntVar(&batchSize, "batch-size", batchDefault, "JSONL processing batch size")
	flags.BoolVar(&dryRun, "dry-run", false, "inspect changes without writing Qdrant")
	flags.BoolVar(&forceReembed, "force-reembed", false, "recalculate every embedding")
	flags.BoolVar(&prune, "prune", false, "delete managed points absent from a single input")
	flags.BoolVar(&continueOnError, "continue-on-error", false, "continue after a failed batch")
	flags.StringVar(&logLevel, "log-level", envString("LOG_LEVEL", "info"), "error, warn, info, or debug")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if (runDirectory == "") == (input == "") {
		return errors.New("exactly one of --run or --input is required")
	}
	if batchSize <= 0 {
		return errors.New("--batch-size must be a positive integer")
	}
	if qdrantTimeout <= 0 || teiTimeout <= 0 {
		return errors.New("--qdrant-timeout and --tei-timeout must be positive")
	}
	if input != "" && strings.TrimSpace(collection) == "" {
		return errors.New("--collection must be non-empty in single-file mode")
	}
	if !validLogLevel(logLevel) {
		return fmt.Errorf("unknown log level %q", logLevel)
	}
	vectorCacheSize := cacheDefault
	common := indexer.RunConfig{
		Contract:  contract,
		QdrantURL: qdrantURL, QdrantAPIKey: strings.TrimSpace(os.Getenv("QDRANT_API_KEY")), QdrantTimeout: qdrantTimeout,
		TEIURL: teiURL, TEITimeout: teiTimeout, BatchSize: batchSize,
		VectorCacheSize: vectorCacheSize, StoreEmbeddingText: storeEmbeddingText, DryRun: dryRun,
		ForceReembed: forceReembed, ContinueOnError: continueOnError, LogLevel: logLevel,
		Progress: func(message string) { fmt.Fprintln(stdout, message) },
	}
	if runDirectory != "" {
		if prune {
			return errors.New("--prune is automatic in --run mode; omit the flag")
		}
		common.RunDir = runDirectory
		common.ReportFile = reportFile
		result, err := indexer.IndexRun(ctx, common, indexer.Dependencies{})
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Status: %s\nWorkspaces: %d successful (%d with warnings), %d failed, %d skipped\nReport: %s\n", result.Status, result.SuccessfulWorkspaces, result.WarningWorkspaces, result.FailedWorkspaces, result.SkippedWorkspaces, reportPath(reportFile, runDirectory, "index-report.json"))
		if result.Status != "complete" && result.Status != "complete_with_warnings" {
			return fmt.Errorf("index run finished with status %s", result.Status)
		}
		return nil
	}
	result, err := indexer.Index(ctx, indexer.Config{
		Contract:  common.Contract,
		InputFile: input, ReportFile: reportFile, Collection: collection,
		QdrantURL: common.QdrantURL, QdrantAPIKey: common.QdrantAPIKey, QdrantTimeout: common.QdrantTimeout,
		TEIURL: common.TEIURL, TEITimeout: common.TEITimeout, BatchSize: common.BatchSize,
		VectorCacheSize:    common.VectorCacheSize,
		StoreEmbeddingText: common.StoreEmbeddingText, DryRun: common.DryRun, ForceReembed: common.ForceReembed,
		Prune: prune, ContinueOnError: common.ContinueOnError, LogLevel: common.LogLevel,
	}, indexer.Dependencies{})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Indexed: %d embedded, %d payload-only, %d unchanged, %d deleted, %d failed\n", result.EmbeddedChunks, result.PayloadOnlyUpdates, result.SkippedUnchanged, result.DeletedChunks, result.FailedChunks)
	if result.FailedChunks > 0 || len(result.Errors) > 0 {
		return errors.New("indexing finished with errors; see the index report")
	}
	return nil
}

func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
func envPositiveInt(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", name, value)
	}
	return parsed, nil
}
func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	if parsed, err := time.ParseDuration(value); err == nil {
		if parsed <= 0 {
			return 0, fmt.Errorf("%s must be positive, got %q", name, value)
		}
		return parsed, nil
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second, nil
	}
	return 0, fmt.Errorf("%s must be a positive duration, got %q", name, value)
}
func envBool(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes":
		return true, nil
	case "0", "false", "no":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be a boolean, got %q", name, value)
	}
}
func validLogLevel(value string) bool {
	return value == "error" || value == "warn" || value == "info" || value == "debug"
}
func reportPath(explicit, root, fallback string) string {
	if explicit != "" {
		return explicit
	}
	return filepath.Join(root, fallback)
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
	fmt.Fprintln(writer, "  fetcher index --run <chunks_run> [--dry-run] [--force-reembed] [--continue-on-error]")
	fmt.Fprintln(writer, "  fetcher index --input <chunks.jsonl> --collection <name> [--prune] [--dry-run]")
}
