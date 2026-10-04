package keybindings

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Errors returned when parsing a binding specification.
var (
	// ErrEmptySpec is returned for an empty binding string.
	ErrEmptySpec = errors.New("keybindings: empty key specification")
	// ErrEmptyBinding is returned when a configuration entry has no keys.
	ErrEmptyBinding = errors.New("keybindings: action must have at least one key")
)

// modAliases maps the spellings accepted in configuration to canonical
// modifiers.
//
// Aliases exist so that configuration can be written the way the user thinks.
// "ctrl", "control", "c-" and "^" are all reasonable names for one modifier,
// and rejecting two of them would be arbitrary. Single-letter aliases are
// limited to c and m because "a" would be indistinguishable from the a key and
// "s" from shift in ordinary use.
var modAliases = map[string]Mod{
	"ctrl": ModCtrl, "control": ModCtrl, "c": ModCtrl, "^": ModCtrl,
	"alt": ModAlt, "option": ModAlt, "opt": ModAlt, "m": ModAlt, "meta": ModAlt,
	"shift": ModShift, "s": ModShift,
	"super": ModSuper, "cmd": ModSuper, "command": ModSuper, "win": ModSuper,
}

// ParseKey parses a single key specification such as "ctrl+q", "mod+enter",
// "shift+tab", "f1" or "j".
//
// "mod" is accepted as a synonym for ctrl, since that is the spelling used
// throughout wterm's documentation and matches the i3wm-inspired vocabulary the
// bindings are designed around.
func ParseKey(spec string) (Key, error) {
	raw := strings.TrimSpace(spec)
	if raw == "" {
		return Key{}, ErrEmptySpec
	}

	var (
		mod  Mod
		part = raw
	)

	// Split on the final "+" so that a literal plus key can still be expressed
	// as "ctrl++". A trailing "++" is the modified plus key, so the separator
	// is the first plus of that trailing pair.
	// "ctrl++" would split to modPart="ctrl+", which is not a modifier. Treat
	// a trailing pair as the separator plus the literal plus key.
	tailStart := strings.LastIndex(raw, "+")
	if tailStart >= 0 && strings.HasSuffix(raw, "++") {
		tailStart--
	}

	if idx := tailStart; idx >= 0 {
		modPart := strings.TrimSpace(raw[:idx])
		tail := strings.TrimSpace(raw[idx+1:])

		// A bare "+" has no modifier part at all and is handled by the
		// single-rune path below.
		if modPart != "" {
			parsed, err := ParseModSpec(modPart)
			if err != nil {
				return Key{}, fmt.Errorf("keybindings: %q in %q: %w", modPart, spec, err)
			}
			mod, part = parsed.Mod(), tail
			if part == "" {
				return Key{}, fmt.Errorf("keybindings: %q has a modifier but no key", spec)
			}
		}
	}

	lower := strings.ToLower(part)

	// "^j" is a prefix form with no separator. Checked before the "+" split so
	// that the caret reads as a modifier rather than as a key.
	if strings.HasPrefix(raw, "^") {
		parsed, ok := parseMod("^")
		if !ok {
			return Key{}, fmt.Errorf("keybindings: unknown modifier %q in %q", "^", spec)
		}
		rest := strings.TrimSpace(raw[1:])
		if rest == "" {
			return Key{}, fmt.Errorf("keybindings: %q has a modifier but no key", spec)
		}
		return finishKey(rest, parsed, spec)
	}

	if canonical, ok := canonicalKeyName(lower); ok {
		return Key{Name: canonical, Mod: mod}, nil
	}

	return finishKey(part, mod, spec)
}

// finishKey validates the key portion of a specification against an already
// resolved modifier.
func finishKey(part string, mod Mod, spec string) (Key, error) {
	if canonical, ok := canonicalKeyName(part); ok {
		return Key{Name: canonical, Mod: mod}, nil
	}

	// A single printable rune.
	if r, size := utf8.DecodeRuneInString(part); size == len(part) && size > 0 {
		if unicode.IsControl(r) {
			return Key{}, fmt.Errorf("keybindings: %q is a control character; use a named key", spec)
		}
		return Key{Rune: r, Mod: mod}, nil
	}

	return Key{}, fmt.Errorf(
		"keybindings: %q is neither a single character nor a known key name", spec,
	)
}

// parseMod resolves one modifier token.
func parseMod(s string) (Mod, bool) {
	switch s {
	case "mod":
		// "mod" is wterm's canonical name for ctrl, so "mod+q" and "ctrl+q"
		// resolve identically.
		return ModCtrl, true
	case "none", "nomod":
		return ModNone, true
	default:
		m, ok := modAliases[s]
		return m, ok
	}
}

// ParseKeys parses a list of specifications, such as those given to a single
// action in the configuration file.
func ParseKeys(specs []string) ([]Key, error) {
	if len(specs) == 0 {
		return nil, ErrEmptyBinding
	}
	keys := make([]Key, 0, len(specs))
	seen := make(map[Key]bool, len(specs))
	for _, spec := range specs {
		k, err := ParseKey(spec)
		if err != nil {
			return nil, fmt.Errorf("%w (in %q)", err, strings.Join(specs, ", "))
		}
		// A repeated key within one action is harmless but almost always a
		// copy-paste error, so reject it rather than storing it twice.
		if seen[k] {
			return nil, fmt.Errorf("keybindings: key %q listed twice for the same action", k)
		}
		seen[k] = true
		keys = append(keys, k)
	}
	return keys, nil
}

// FormatKeys renders a key list the way it is written in configuration, so
// that defaults can be serialised back to TOML without a separate translation.
func FormatKeys(keys []Key) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k.String())
	}
	return out
}
