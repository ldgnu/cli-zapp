// Command cli-zapp is a WhatsApp client for the terminal.
//
// See docs/ARCHITECTURE.md for the layering and docs/LIMITATIONS.md for what the
// protocol does not support.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/cli-zapp/cli-zapp/internal/keybindings"
	"github.com/cli-zapp/cli-zapp/internal/logging"
	"github.com/cli-zapp/cli-zapp/internal/notifications"
	"github.com/cli-zapp/cli-zapp/internal/store"
	"github.com/cli-zapp/cli-zapp/internal/ui/app"
	"github.com/cli-zapp/cli-zapp/internal/ui/theme"
	"github.com/cli-zapp/cli-zapp/internal/whatsapp"
	"github.com/cli-zapp/cli-zapp/internal/whatsapp/adapter"
)

// Build metadata, injected at link time with -ldflags "-X main.<name>=...".
//
// All three have a working default so that `go run` and `go build ./...` without
// -ldflags still produce a binary that answers --version honestly rather than
// printing nothing.
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

// config is the runtime configuration, assembled from flags.
type config struct {
	debug   bool
	verbose bool
	logFile string

	// demo runs against the in-memory fake instead of the network.
	//
	// Phase 1 ships with no protocol adapter, so this is the default. When the
	// adapter lands it becomes opt-in, and the flag stays so that the UI can be
	// exercised without linking an account.
	//
	// Still the default: pairing is irreversible, and a program that contacts
	// WhatsApp on startup without being asked would be a bad neighbour on a shared
	// machine.
	demo bool

	// live runs against a linked account. Opt-in, because asking for the network has
	// to be a deliberate act rather than the absence of a flag.
	live bool

	color    string
	glyphs   string
	ascii    bool
	showHelp bool

	// showVersion prints build metadata and exits.
	showVersion bool
}

func main() {
	if err := run(); err != nil {
		// Errors go to stderr with a plain prefix: the alternate screen has
		// already been torn down by the time this runs, so ANSI styling would
		// only get in the way.
		fmt.Fprintln(os.Stderr, "cli-zapp:", err)
		os.Exit(1)
	}
}

func run() error {
	// Subcommands are checked before flags.
	//
	// They have to be, because `pair` and `unpair` are the two operations that change
	// something irreversible on someone else's account, and routing them through the
	// flag parser would let `cli-zapp --phone +549... pair` mean something other than
	// what it reads as.
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return runSubcommand(args[0], args[1:])
	}

	cfg := parseFlags()

	log, closeLog, err := logging.New(logging.Options{
		Debug:   cfg.debug,
		Verbose: cfg.verbose,
		File:    cfg.logFile,
	})
	if err != nil {
		return err
	}
	defer closeLog()

	log.Info("cli-zapp starting", "version", version, "demo", cfg.demo)
	defer log.Info("cli-zapp stopped")

	notifier := notifications.New(notifications.Options{
		Bell:      true,
		Desktop:   true,
		Available: notifications.DesktopAvailable(),
	})

	t, err := buildTheme(cfg)
	if err != nil {
		return err
	}

	keys := keybindings.DefaultMap()
	if err := keys.Check(); err != nil {
		// A conflicting default table is a build-time defect, not something a
		// user can fix; failing loudly beats a key that silently does nothing.
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	deps := whatsapp.Services{}

	switch {
	case cfg.live:
		deps, err = liveServices(ctx)
		if err != nil {
			return err
		}
	case cfg.demo:
		fake := app.DemoData()
		deps = fake.Services()
	}

	model := app.New(deps, t, keys)
	model.SetNotifier(notifier)

	// The event stream is started by the model's own Init, which also arms the read
	// that drains it. Starting it here instead would mean two places that have to
	// agree about when the stream begins, and the model's Init is the one that runs
	// first and unconditionally.
	program := tea.NewProgram(model,
		tea.WithContext(ctx),
		tea.WithFPS(60),
	)

	log.Info("starting bubbletea program", "demo", cfg.demo)

	final, err := program.Run()
	if err != nil {
		return fmt.Errorf("running interface: %w", err)
	}

	if m, ok := final.(*app.Model); ok {
		if err := m.Shutdown(); err != nil {
			log.Warn("shutdown error", "error", err.Error())
		}
	}

	// Anything the notifier queued is flushed here so a notification raised in
	// the final moments is not lost.
	notifier.Flush()
	return nil
}

// parseFlags reads the command line.
//
// Flags are defined here rather than in a shared package because the set is
// small and a user reading `cli-zapp --help` should not need to cross-reference
// another file to learn what exists.
func parseFlags() config {
	var cfg config

	fs := flag.NewFlagSet("cli-zapp", flag.ContinueOnError)
	fs.BoolVar(&cfg.debug, "debug", false, "write debug-level logs")
	fs.BoolVar(&cfg.verbose, "verbose", false, "write verbose logs")
	fs.StringVar(&cfg.logFile, "log", "", "log file path (default: a temporary file)")

	fs.BoolVar(&cfg.demo, "demo", true, "run against in-memory demo data")
	fs.BoolVar(&cfg.live, "live", false, "run against a linked WhatsApp account")
	fs.BoolVar(&cfg.showHelp, "help-keys", false, "print the default keybindings and exit")
	fs.BoolVar(&cfg.showVersion, "version", false, "print version information and exit")

	fs.StringVar(&cfg.color, "color", "dark", "color scheme: dark or light")
	fs.StringVar(&cfg.glyphs, "glyphs", "unicode", "glyph set: unicode or ascii")
	fs.BoolVar(&cfg.ascii, "ascii", false, "shorthand for --glyphs=ascii")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "cli-zapp %s — a WhatsApp client for the terminal\n\n", version)
		fmt.Fprint(fs.Output(), `Usage:
  cli-zapp [flags]
  cli-zapp pair --phone <número>
  cli-zapp unpair
  cli-zapp keys

Subcommands:
  pair      link this device to an account
  unpair    unlink this device
  keys      print the default keybindings as TOML

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(os.Args[1:]); err != nil {
		// ContinueOnError already printed the problem; exiting 0 here would be
		// wrong for a bad flag, so the caller sees the usage text and exits 1.
		os.Exit(2)
	}

	// --version is checked before the interface starts, and before the log file is
	// opened, so that asking a binary what it is never creates a log file as a side
	// effect. A `cli-zapp --version` that leaves a temp file behind is the kind of
	// thing that makes people distrust the rest of the tooling.
	if cfg.showVersion {
		printVersion()
		os.Exit(0)
	}

	if cfg.showHelp {
		printKeybindings()
		os.Exit(0)
	}

	if cfg.ascii {
		cfg.glyphs = "ascii"
	}

	return cfg
}

// buildTheme assembles the theme from the configuration flags.
func buildTheme(cfg config) (theme.Theme, error) {
	name, ok := theme.ParseName(cfg.color)
	if !ok {
		return theme.Theme{}, fmt.Errorf("unknown color scheme %q: use dark or light", cfg.color)
	}

	t, ok := theme.Dark().WithName(name)
	if !ok {
		return theme.Theme{}, fmt.Errorf("unknown color scheme %q", cfg.color)
	}

	switch cfg.glyphs {
	case "ascii":
		t = t.WithGlyphs(theme.ASCIIGlyphs())
	case "unicode":
		// Already the default.
	default:
		return theme.Theme{}, fmt.Errorf("unknown glyph set %q: use unicode or ascii", cfg.glyphs)
	}

	return t, nil
}

// printVersion writes the build metadata.
//
// The format is one `key: value` per line rather than a single line, because
// `cli-zapp --version | grep` and `dpkg -s` both want to pick a field out of it,
// and a release page wants to paste it into a bug report verbatim.
func printVersion() {
	fmt.Printf("cli-zapp %s\n", version)
	fmt.Printf("  commit:     %s\n", commit)
	fmt.Printf("  built:      %s\n", buildDate)
	fmt.Printf("  go:         %s\n", runtime.Version())
	fmt.Printf("  platform:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
}

// printKeybindings writes the default bindings to stdout.
//
// The output is TOML-shaped rather than a table so that it can be pasted into a
// configuration file and then edited, which is the point of making the bindings
// data in the first place.
func printKeybindings() {
	fmt.Println("# cli-zapp default keybindings")
	fmt.Println("# Paste into your config file to override them.")
	fmt.Println()

	sections := map[string][]keybindings.Binding{}
	order := make([]string, 0, 8)
	for _, b := range keybindings.Defaults() {
		section, _, _ := cut(b.Action)
		if _, seen := sections[section]; !seen {
			order = append(order, section)
		}
		sections[section] = append(sections[section], b)
	}

	for _, section := range order {
		fmt.Printf("[keys.%s]\n", section)
		for _, b := range sections[section] {
			keys := keybindings.FormatKeys(b.Keys)
			if len(keys) == 1 {
				fmt.Printf("  %-28s = %q\n", b.Action, keys[0])
				continue
			}
			fmt.Printf("  %-28s = [", b.Action)
			for i, k := range keys {
				if i > 0 {
					fmt.Print(", ")
				}
				fmt.Printf("%q", k)
			}
			fmt.Println("]")
		}
		fmt.Println()
	}
}

// cut splits an action name into its section and remainder.
func cut(a keybindings.Action) (string, string, bool) {
	s := string(a)
	for i := range len(s) {
		if s[i] == '.' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

// runSubcommand dispatches a subcommand.
//
// It returns an error rather than calling os.Exit, so that the deferred log close and
// the deferred cancel in run() still happen. A subcommand that skipped them would leave
// a log file open and, worse, would have to duplicate the exit-code logic.
func runSubcommand(name string, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	switch name {
	case "pair":
		return runPair(ctx, args)
	case "unpair":
		return runUnpair(ctx)
	case "keys":
		printKeybindings()
		return nil
	case "help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown subcommand %q; try: pair, unpair, keys", name)
	}
}

// printUsage writes the help text without a flag set.
func printUsage() {
	cfg := parseFlags
	_ = cfg
	fmt.Print(`cli-zapp — a WhatsApp client for the terminal

Usage:
  cli-zapp [flags]
  cli-zapp pair --phone <número>
  cli-zapp unpair
  cli-zapp keys

Flags:
  --live                run against a linked account (default: demo data)
  --demo                run against in-memory demo data
  --version             print version information
  --color=dark|light    colour scheme
  --glyphs=unicode|ascii
  --debug, --verbose    log level
  --log=PATH            log file
  --help-keys           print the default keybindings as TOML

Read SECURITY.md before linking an account: this speaks an unofficial protocol and
carries a real risk of a temporary or permanent account ban.
`)
}

// liveServices opens the device store, connects, and returns the service bundle.
//
// The order matters and is not interchangeable. The store is opened first because it is
// what tells us whether a device is linked at all; connecting before that check would
// mean opening a websocket for an account that does not exist yet.
//
// A missing pairing is reported with the exact command to run, not as a generic
// failure. "Not paired" with no next step is the kind of error that costs someone half
// an hour of reading documentation.
func liveServices(ctx context.Context) (whatsapp.Services, error) {
	paths, err := store.Default()
	if err != nil {
		return whatsapp.Services{}, err
	}
	if err := paths.Ensure(); err != nil {
		return whatsapp.Services{}, err
	}

	client, err := adapter.New(ctx, paths, waLog.Noop)
	if err != nil {
		return whatsapp.Services{}, fmt.Errorf("opening the device store: %w", err)
	}

	if !client.Paired() {
		_ = client.Close()
		return whatsapp.Services{}, fmt.Errorf(
			"no hay ninguna cuenta enlazada en %s\n"+
				"       vinculá una con:  cli-zapp pair --phone +<código de país y número>",
			paths.Devices)
	}

	if _, err := client.Connect(ctx); err != nil {
		_ = client.Close()
		return whatsapp.Services{}, fmt.Errorf("conectando: %w", err)
	}

	services := adapter.NewServices(client)
	if err := services.Sync.Start(ctx); err != nil {
		_ = client.Close()
		return whatsapp.Services{}, err
	}
	return services, nil
}
