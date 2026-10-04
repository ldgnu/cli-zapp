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
	"syscall"

	tea "charm.land/bubbletea/v2"

	"github.com/cli-zapp/cli-zapp/internal/keybindings"
	"github.com/cli-zapp/cli-zapp/internal/logging"
	"github.com/cli-zapp/cli-zapp/internal/notifications"
	"github.com/cli-zapp/cli-zapp/internal/ui/app"
	"github.com/cli-zapp/cli-zapp/internal/ui/theme"
	"github.com/cli-zapp/cli-zapp/internal/whatsapp"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

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
	demo bool

	color    string
	glyphs   string
	ascii    bool
	showHelp bool
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

	deps := whatsapp.Services{}

	if cfg.demo {
		fake := app.DemoData()
		deps = fake.Services()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

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
	fs.BoolVar(&cfg.showHelp, "help-keys", false, "print the default keybindings and exit")

	fs.StringVar(&cfg.color, "color", "dark", "color scheme: dark or light")
	fs.StringVar(&cfg.glyphs, "glyphs", "unicode", "glyph set: unicode or ascii")
	fs.BoolVar(&cfg.ascii, "ascii", false, "shorthand for --glyphs=ascii")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "cli-zapp %s — a WhatsApp client for the terminal\n\n", version)
		fmt.Fprintf(fs.Output(), "Usage:\n  cli-zapp [flags]\n\nFlags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(os.Args[1:]); err != nil {
		// ContinueOnError already printed the problem; exiting 0 here would be
		// wrong for a bad flag, so the caller sees the usage text and exits 1.
		os.Exit(2)
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
