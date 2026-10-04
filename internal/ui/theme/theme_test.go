package theme

import (
	"image/color"
	"testing"
)

func TestDarkAndLightPalettesAreFullyPopulated(t *testing.T) {
	// A nil colour makes a style silently invisible, which is the kind of defect
	// that only shows up on one background. Every field is checked.
	for name, p := range map[string]Palette{"dark": DarkPalette(), "light": LightPalette()} {
		fields := map[string]color.Color{
			"Accent": p.Accent, "AccentMuted": p.AccentMuted, "Foreground": p.Foreground,
			"SidebarBackground": p.SidebarBackground, "SidebarSelected": p.SidebarSelected,
			"SidebarSelectedFG": p.SidebarSelectedFG, "SidebarTitle": p.SidebarTitle,
			"HeaderBackground": p.HeaderBackground, "HeaderForeground": p.HeaderForeground,
			"Muted": p.Muted, "Faint": p.Faint,
			"OutgoingBG": p.OutgoingBG, "OutgoingFG": p.OutgoingFG,
			"IncomingBG": p.IncomingBG, "IncomingFG": p.IncomingFG,
			"Border": p.Border, "BorderFocus": p.BorderFocus,
			"StatusBarFG":     p.StatusBarFG,
			"ModalBackground": p.ModalBackground, "ModalBorder": p.ModalBorder,
			"Delivered": p.Delivered, "Read": p.Read, "Failed": p.Failed,
			"SystemText": p.SystemText, "Mention": p.Mention, "SearchMatch": p.SearchMatch,
			"Online": p.Online, "Offline": p.Offline,
		}
		for field, c := range fields {
			if c == nil {
				t.Errorf("%s palette: %s is nil", name, field)
			}
		}
	}
}

func TestPalettesDiffer(t *testing.T) {
	// Two identical palettes would mean one of them was never written.
	// Background is deliberately absent from the palette so the terminal's own
	// colours show through; there is nothing to compare for it.
	dark, light := DarkPalette(), LightPalette()
	if dark.Foreground == light.Foreground {
		t.Error("the two palettes should not share a foreground colour")
	}
}

func TestPaletteByName(t *testing.T) {
	if _, ok := PaletteByName(NameDark); !ok {
		t.Error("dark should resolve")
	}
	if _, ok := PaletteByName(NameLight); !ok {
		t.Error("light should resolve")
	}
	if _, ok := PaletteByName("neon"); ok {
		t.Error("an unknown name should not resolve")
	}
}

func TestParseName(t *testing.T) {
	if n, ok := ParseName("dark"); !ok || n != NameDark {
		t.Errorf("got %v, %v", n, ok)
	}
	if _, ok := ParseName("nope"); ok {
		t.Error("an unknown name should be rejected")
	}
}

func TestDefaultMetricsAreValid(t *testing.T) {
	if !DefaultMetrics().Validate() {
		t.Error("the default metrics should validate")
	}
}

func TestMetricsValidate(t *testing.T) {
	// A valid base that each case perturbs in one way, so a failure names the
	// field at fault rather than a wall of literals.
	valid := Metrics{
		SidebarWidth: 30, HeaderHeight: 1, StatusHeight: 1, ComposerHeight: 3,
		MinWidth: 40, MinHeight: 10, NarrowWidth: 60,
	}
	perturb := func(fn func(*Metrics)) Metrics {
		m := valid
		fn(&m)
		return m
	}

	tests := []struct {
		name string
		m    Metrics
		want bool
	}{
		{"defaults", DefaultMetrics(), true},
		{"explicitly valid", valid, true},
		{"zero sidebar", perturb(func(m *Metrics) { m.SidebarWidth = 0 }), false},
		{"zero height", perturb(func(m *Metrics) { m.HeaderHeight = 0 }), false},
		{"negative", perturb(func(m *Metrics) { m.SidebarWidth = -1 }), false},
		{"chrome exceeds terminal", perturb(func(m *Metrics) {
			m.HeaderHeight, m.StatusHeight, m.ComposerHeight = 5, 5, 5
		}), false},
		{"zero minimums", perturb(func(m *Metrics) {
			m.MinWidth, m.MinHeight, m.NarrowWidth = 0, 0, 0
		}), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.m.Validate(); got != tc.want {
				t.Errorf("Validate() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSidebarVisibleThreshold(t *testing.T) {
	th := Dark()

	// At or below the narrow threshold the sidebar is dropped rather than
	// squeezed: a three-column sidebar is noise, not information.
	if th.SidebarVisible(th.Metrics.NarrowWidth) {
		t.Errorf("the sidebar should be hidden at exactly the threshold (%d)", th.Metrics.NarrowWidth)
	}
	if !th.SidebarVisible(th.Metrics.NarrowWidth + 1) {
		t.Error("the sidebar should be visible just above the threshold")
	}
	if !th.SidebarVisible(200) {
		t.Error("the sidebar should be visible at a normal width")
	}
}

func TestContentWidthAccountsForTheSidebar(t *testing.T) {
	th := Dark()

	wide := th.ContentWidth(th.Metrics.NarrowWidth + 10)
	if wide != th.Metrics.SidebarWidth+10-1 {
		t.Errorf("wide content width = %d, want %d", wide, th.Metrics.SidebarWidth+9)
	}

	narrow := th.ContentWidth(th.Metrics.NarrowWidth - 10)
	if narrow != th.Metrics.NarrowWidth-10 {
		t.Errorf("narrow content width should be the full terminal, got %d", narrow)
	}
}

func TestContentHeight(t *testing.T) {
	th := Dark()
	const h = 24

	want := h - th.Metrics.HeaderHeight - th.Metrics.ComposerHeight - th.Metrics.StatusHeight
	if got := th.ContentHeight(h); got != want {
		t.Errorf("ContentHeight(%d) = %d, want %d", h, got, want)
	}
}

func TestFits(t *testing.T) {
	th := Dark()

	// The minimum is by definition the smallest size that fits: Metrics.Validate
	// guarantees the fixed chrome still leaves room for the conversation.
	if !th.Fits(th.Metrics.MinWidth, th.Metrics.MinHeight) {
		t.Error("the declared minimum should fit")
	}
	if !th.Fits(100, 40) {
		t.Error("a normal terminal should fit")
	}
	if th.Fits(th.Metrics.MinWidth-1, th.Metrics.MinHeight) {
		t.Error("one column below the minimum should not fit")
	}
	if th.Fits(th.Metrics.MinWidth, th.Metrics.MinHeight-1) {
		t.Error("one line below the minimum should not fit")
	}
	if th.Fits(0, 0) {
		t.Error("a zero-size terminal should not fit")
	}
}

func TestWithName(t *testing.T) {
	th := Dark()

	light, ok := th.WithName(NameLight)
	if !ok {
		t.Fatal("light should resolve")
	}
	if light.Name != NameLight {
		t.Errorf("name = %q, want light", light.Name)
	}
	if th.Name != NameDark {
		t.Error("WithName must not mutate the receiver")
	}

	if _, ok := th.WithName("neon"); ok {
		t.Error("an unknown name should be rejected")
	}
}

func TestWithMetricsIgnoresInvalid(t *testing.T) {
	th := Dark()
	same := th.WithMetrics(Metrics{})
	if same.Metrics != th.Metrics {
		t.Error("invalid metrics should be ignored rather than applied")
	}

	wider := th.WithMetrics(Metrics{
		SidebarWidth: 40, HeaderHeight: 2, StatusHeight: 1, ComposerHeight: 4,
		MinWidth: 40, MinHeight: 12, NarrowWidth: 60,
	})
	if wider.Metrics.SidebarWidth != 40 {
		t.Errorf("valid metrics should be applied, got %d", wider.Metrics.SidebarWidth)
	}
}

func TestStylesAreDerived(t *testing.T) {
	// Every style must exist; a zero lipgloss.Style renders unstyled, which would
	// look like a colour that silently stopped working.
	th := Dark()
	if th.Styles.ChatRowSelected.GetForeground() == nil {
		t.Error("the selected row should have a foreground colour")
	}
	if th.Styles.StatusBar.GetForeground() == nil {
		t.Error("the status bar should have a foreground colour")
	}
	if th.Styles.Error.GetForeground() != DarkPalette().Failed {
		t.Error("the error style should use the palette's failed colour")
	}
}

func TestWithGlyphsSwitchesTheSet(t *testing.T) {
	th := Dark()
	ascii := th.WithGlyphs(ASCIIGlyphs())

	if th.Glyphs.Bullet != "•" {
		t.Error("WithGlyphs must not mutate the receiver")
	}
	if ascii.Glyphs.Bullet != "*" {
		t.Errorf("ASCII bullet = %q, want *", ascii.Glyphs.Bullet)
	}

	for _, g := range []Glyphs{UnicodeGlyphs(), ASCIIGlyphs()} {
		for name, v := range map[string]string{
			"Divider": g.Divider, "Placeholder": g.Placeholder, "Bullet": g.Bullet,
			"Chevron": g.Chevron, "Close": g.Close, "Warn": g.Warn,
		} {
			if v == "" {
				t.Errorf("glyph %q is empty in the set", name)
			}
		}
	}
}

func TestASCIIGlyphsAreASCII(t *testing.T) {
	// The whole point of the ASCII set is that a terminal without Unicode
	// coverage renders something legible rather than replacement boxes.
	for name, v := range map[string]string{
		"Divider": ASCIIGlyphs().Divider, "Placeholder": ASCIIGlyphs().Placeholder,
		"Scrollbar": ASCIIGlyphs().Scrollbar, "Chevron": ASCIIGlyphs().Chevron,
	} {
		for _, r := range v {
			if r > 0x7f {
				t.Errorf("ASCII glyph %q contains %q", name, r)
			}
		}
	}
}
