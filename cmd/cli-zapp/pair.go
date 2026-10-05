package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cli-zapp/cli-zapp/internal/store"
	"github.com/cli-zapp/cli-zapp/internal/whatsapp/adapter"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// pairCmd links this device to a WhatsApp account.
//
// It is a subcommand rather than a flag because it is a one-off with a consequence —
// it registers this machine as a linked device on someone's account — and a thing like
// that should be something you type deliberately, not something reachable from a flag
// sweep.
//
// # The warning comes first
//
// Before anything touches the network. The user is told what is about to happen, in
// plain words, and asked to confirm. A warning printed after the QR code appears is a
// warning nobody reads, and the whole point is that this is the last moment at which
// choosing a secondary number is still free.
const riskWarning = `
  ╭──────────────────────────────────────────────────────────────╮
  │  Esto enlaza ESTE equipo a tu cuenta de WhatsApp             │
  ├──────────────────────────────────────────────────────────────┤
  │                                                              │
  │  cli-zapp habla un protocolo no oficial. Meta lo dice        │
  │  explícitamente: enlazar una app no autorizada viola los     │
  │  Términos de Servicio y puede terminar en un baneo.          │
  │                                                              │
  │  El peor caso no es que te baneen a vos: es que la cuenta   │
  │  quede sin poder enlazar NINGÚN cliente, ni siquiera el      │
  │  WhatsApp Web oficial. Ya pasó con otra herramienta         │
  │  (sept 2025 – feb 2026, cinco meses sin archivar).          │
  │                                                              │
  │  → Usá un número secundario. Cuesta casi nada y es lo       │
  │    único que elimina el riesgo en vez de reducirlo.          │
  │                                                              │
  ╰──────────────────────────────────────────────────────────────╯
`

// runPair implements `cli-zapp pair`.
func runPair(ctx context.Context, args []string) error {
	phone := flagString(args, "--phone")
	skipWarning := hasFlag(args, "--yes")

	if !skipWarning {
		fmt.Fprintln(os.Stderr, strings.TrimRight(riskWarning, "\n"))
		ok, err := confirm("¿Continuar? [s/N] ")
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(os.Stderr, "\nCancelado. No se enlazó nada.")
			return nil
		}
	}

	paths, err := store.Default()
	if err != nil {
		return err
	}
	if err := paths.Ensure(); err != nil {
		return err
	}

	log := waLog.Noop
	client, err := adapter.New(ctx, paths, log)
	if err != nil {
		return err
	}
	defer client.Close()

	if client.Paired() {
		fmt.Fprintf(os.Stderr, "Ya hay una cuenta enlazada (%s).\n", client.DeviceName())
		fmt.Fprintln(os.Stderr, "Usá `cli-zapp unpair` si querés cambiarla.")
		return nil
	}

	fmt.Fprintf(os.Stderr, "Abriendo el almacén en %s\n", paths.Devices)

	// The phone-number route is the default rather than the QR one.
	//
	// A terminal in an SSH session, or one narrower than a QR code is legible, cannot
	// scan one — and this program cannot tell the difference reliably. The code takes
	// eight keystrokes on the phone and works everywhere.
	if phone == "" {
		var err error
		phone, err = prompt("Número con código de país, para escribir el código a mano (vacío para QR): ")
		if err != nil {
			return err
		}
	}

	if strings.TrimSpace(phone) != "" {
		return pairByPhone(ctx, client, strings.TrimSpace(phone))
	}
	return pairByQR(ctx, client)
}

// pairByPhone prints a pairing code to type on the phone.
func pairByPhone(ctx context.Context, client *adapter.Client, phone string) error {
	fmt.Fprintln(os.Stderr, "\nPidiendo el código de emparejamiento…")

	code, err := client.PhoneMethod(ctx, phone)
	if err != nil {
		return err
	}
	if code == "" {
		fmt.Fprintln(os.Stderr, "La cuenta ya estaba enlazada.")
		return nil
	}

	fmt.Fprintf(os.Stderr, `
  En WhatsApp:  Ajustes → Dispositivos vinculados → Enlazar dispositivo
  Elegí "Vincular con número de teléfono" y escribí este código:

        ┌───────────────────┐
        │  %-17s │
        └───────────────────┘
`, chunk(code))

	fmt.Fprintln(os.Stderr, "  Esperando…")
	if err := waitPaired(ctx, client, 60*time.Second); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\n  ✓ Enlazado como %s\n", client.JID())
	fmt.Fprintln(os.Stderr, "  Ahora: cli-zapp --live")
	return nil
}

// pairByQR explains why there is no QR code, and points at the route that works.
//
// # Why there is no QR here
//
// A QR encoder is about three hundred lines and cannot be verified in this repository:
// nothing here can scan a code, so an encoder with a subtle error — a wrong format
// string, a bad mask choice — produces a picture that looks exactly right and does not
// scan. The phone simply sees nothing, and the user has no way to tell that the code is
// broken rather than that they moved their phone.
//
// That failure mode is worse than not offering the feature. The pairing code takes
// eight keystrokes on the phone and works in every terminal, including one over SSH
// with no graphics at all.
//
// Offering QR later means either a dependency with a test suite behind it, or an
// encoder with a golden-vector test: a known payload encoded to a known matrix,
// compared against a reference produced by a different implementation. Anything less is
// a code that works until it does not.
func pairByQR(context.Context, *adapter.Client) error {
	fmt.Fprint(os.Stderr, `
  No hay código QR: se necesita un terminal con gráficos y un encoder verificado
  contra una implementación independiente, y ninguna de las dos cosas existe acá.

  Usá el código de emparejamiento, que funciona en cualquier terminal:

      cli-zapp pair --phone +5491100000000

`)
	return errNoQR
}

// waitPaired polls until the device is linked or the deadline passes.
//
// Polling rather than a channel because the confirmation arrives as a protocol event
// and wiring a handler for the one-shot case would mean a handler that has to be
// removed again; the interval is short enough that nobody can tell the difference.
func waitPaired(ctx context.Context, client *adapter.Client, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if client.Paired() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("venció la espera sin vincular: %w", adapter.ErrPairingTimeout)
}

// chunk spaces out a code so it can be read aloud or typed carefully.
//
// The width is derived from the code, never assumed: upstream documents neither the
// length of a pairing code nor its expiry, so hardcoding "8 digits" would render a
// wrong number of characters the day that changes.
func chunk(code string) string {
	runes := []rune(code)
	if len(runes) <= 1 {
		return code
	}

	// Split into two equal halves rather than fixed groups of four.
	//
	// Grouping by four leaves a ragged tail whenever the length is not a multiple of
	// four — and the real code came back as eight characters, so the groups were
	// "P97Y -YSY G", with a lone letter stranded on the end. Two halves always look
	// deliberate, and they work whatever the length turns out to be.
	half := (len(runes) + 1) / 2
	return string(runes[:half]) + " " + string(runes[half:])
}

// confirm asks a yes/no question on stderr.
//
// A closed stdin counts as "no". That default matters: this prompt guards an
// irreversible action on someone's account, so the case where there is nobody to answer
// must resolve to doing nothing rather than to proceeding — including when it is piped
// from /dev/null by a script that did not think about the question.
func confirm(prompt string) (bool, error) {
	fmt.Fprint(os.Stderr, prompt)

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	switch {
	case err != nil && line == "":
		return false, nil
	case err != nil:
		return false, fmt.Errorf("leyendo la respuesta: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(line)) {
	case "s", "si", "sí", "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// prompt asks for a value on stderr, returning "" on an empty line or a closed stdin.
func prompt(label string) (string, error) {
	fmt.Fprint(os.Stderr, "  "+label)

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	switch {
	case err != nil && line == "":
		return "", nil
	case err != nil:
		return "", fmt.Errorf("leyendo la entrada: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// runUnpair implements `cli-zapp unpair`.
func runUnpair(ctx context.Context) error {
	paths, err := store.Default()
	if err != nil {
		return err
	}
	client, err := adapter.New(ctx, paths, waLog.Noop)
	if err != nil {
		return err
	}
	defer client.Close()

	if !client.Paired() {
		fmt.Fprintln(os.Stderr, "No hay ninguna cuenta enlazada.")
		return nil
	}

	fmt.Fprintln(os.Stderr, `
  Esto desenlaza ESTE equipo de la cuenta. El teléfono va a olvidar este
  dispositivo y hay que vincularlo otra vez con un código nuevo.

  No banea la cuenta: es lo contrario, es como se limpia un enlace.

  ¿Continuar? [s/N] `)
	ok, err := confirm("")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(os.Stderr, "Cancelado.")
		return nil
	}

	if err := client.Logout(ctx); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "\n  ✓ Desenlazado.")
	return nil
}

// hasFlag reports whether a bare flag is present.
func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

// flagString reads a flag in either form.
//
// Both "--phone +549..." and "--phone=+549..." are accepted, because people write
// whichever comes to mind and the second one is not what anyone types interactively.
// Accepting only the equals form meant the documented example — `pair --phone <número>`,
// the form printed in the warning — did not work.
func flagString(args []string, name string) string {
	for i, a := range args {
		if a == name {
			// Separate value: the next argument, if there is one.
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				return args[i+1]
			}
			return ""
		}
		if strings.HasPrefix(a, name+"=") {
			return strings.TrimPrefix(a, name+"=")
		}
	}
	return ""
}

// errNoQR is returned when QR pairing is asked for.
//
// A distinct error rather than a nil return so the exit code is non-zero and the
// explanation above actually reaches the user instead of the program appearing to
// succeed while doing nothing.
var errNoQR = errors.New("QR pairing is not available; use --phone")
