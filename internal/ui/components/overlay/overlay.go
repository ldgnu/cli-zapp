// Package overlay implements modals, menus and toasts.
//
// # A stack, not a flag
//
// Overlays nest: a context menu opens over the conversation, and a delete
// confirmation opens over the menu. A boolean "modalOpen" cannot express that.
// [Stack] can, and because it is an ordinary slice the whole model is still a
// value that Update returns by copy.
//
// # Rendering onto a canvas
//
// Every overlay is drawn onto a canvas of exactly the terminal's size rather than
// being returned as a free-floating box. Two properties fall out of that, both of
// which a TUI needs and neither of which is obvious until it has gone wrong:
//
//   - An overlay taller or wider than the terminal is clipped rather than
//     spilling past the screen edge and corrupting the scrollback the user sees on
//     exit.
//   - The result always has exactly width×height lines, so the caller can compose
//     it over a background without special-casing sizes.
package overlay

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/wterm/wterm/internal/text"
	"github.com/wterm/wterm/internal/ui/theme"
)

// Kind distinguishes the overlay types, because they behave differently: a menu
// consumes up and down, a modal does not, a toast consumes nothing.
type Kind int

// Recognised overlay kinds.
const (
	// KindMenu is a navigable list of actions.
	KindMenu Kind = iota
	// KindModal is a block of content with optional buttons.
	KindModal
	// KindToast is a transient notification with no interaction.
	KindToast
)

// MenuItem is one entry in a [KindMenu].
type MenuItem struct {
	// Label is the display text.
	Label string
	// Hint is an optional right-aligned shortcut, e.g. "mod+d".
	Hint string
	// Disabled greys the item out and makes it non-selectable.
	Disabled bool
	// Danger marks a destructive action such as deleting a message.
	Danger bool
	// Separator renders a divider instead of an item.
	Separator bool
}

// Separator returns a divider entry for use between menu items.
//
// It is a constructor rather than a struct literal so that the Separator flag
// stays an implementation detail of this package.
func Separator() MenuItem { return MenuItem{Separator: true} }

// Overlay is a single layer.
type Overlay struct {
	// Kind is what the renderers switch on. The other fields are interpreted
	// according to it.
	Kind Kind

	Title string
	// Content is a single-line message, used by toasts and confirmations.
	Content string
	// Body is multi-line content, used by information modals.
	Body []string

	Items  []MenuItem
	Cursor int

	Buttons []MenuItem

	// CancelDefault marks a modal whose safe button starts focused.
	CancelDefault bool
	// Danger styles the overlay as destructive.
	Danger bool
	// ExpiresAt is when a toast should disappear. Zero for overlays that persist.
	ExpiresAt time.Time
}

// Menu builds a menu overlay from a title and items.
func Menu(title string, items []MenuItem, cursor int) Overlay {
	return Overlay{Kind: KindMenu, Title: title, Items: items, Cursor: cursor}
}

// Confirm builds a yes/no modal.
func Confirm(title, message string) Overlay {
	return Overlay{
		Kind:    KindModal,
		Title:   title,
		Content: message,
		Buttons: []MenuItem{{Label: "Cancel"}, {Label: "Confirm", Danger: true}},
		Cursor:  0,
		// Cancel is focused by default, so that an accidental enter cannot destroy
		// data.
		CancelDefault: true,
	}
}

// Info builds a read-only modal.
func Info(title string, lines []string) Overlay {
	return Overlay{Kind: KindModal, Title: title, Body: lines}
}

// DefaultToastTTL is how long a toast stays on screen.
const DefaultToastTTL = 4 * time.Second

// Toast builds a transient notification with the default lifetime.
func Toast(msg string, isError bool) Overlay {
	return ToastFor(msg, isError, DefaultToastTTL)
}

// ToastFor builds a transient notification with an explicit lifetime.
//
// The expiry lives on the overlay rather than in a parallel timer because the
// toast is the thing that expires. A timer tracked separately would have to be
// cancelled when the user dismisses the toast, and a missed cancellation would
// leave the model popping something that is no longer on screen.
func ToastFor(msg string, isError bool, ttl time.Duration) Overlay {
	return Overlay{
		Kind:      KindToast,
		Content:   msg,
		Danger:    isError,
		ExpiresAt: time.Now().Add(ttl),
	}
}

// MaxWidth caps how wide an overlay may grow.
//
// Without a cap a long line would stretch across the terminal and push the layout
// around; overlays are meant to feel like layers, not pages.
const MaxWidth = 72

// View renders a single overlay onto a canvas of exactly the terminal's size.
func View(o Overlay, width, height int, t theme.Theme) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	canvas := blankCanvas(width, height)

	// Toasts are anchored to the bottom rather than centred, so that a
	// notification never covers the conversation being read.
	if o.Kind == KindToast {
		return paint(canvas, bottomAnchored(viewToast(o, width, t), width), height)
	}

	return paint(canvas, centre(viewBox(o, width, t), height), height)
}

// viewBox renders the box for a menu or modal, without placement.
//
// Placement belongs to [View]: a box has no opinion about where on the screen it
// goes, and doing it in one place means a new overlay kind cannot get it wrong.
func viewBox(o Overlay, width int, t theme.Theme) string {
	switch o.Kind {
	case KindMenu:
		return viewMenu(o, width, t)
	case KindModal:
		return viewModal(o, width, t)
	case KindToast:
		return viewToast(o, width, t)
	default:
		return ""
	}
}

func viewMenu(o Overlay, width int, t theme.Theme) string {
	st := t.Styles

	inner := min(width-8, MaxWidth-2)
	if inner < 10 {
		inner = max(width, 10)
	}

	var lines []string
	if o.Title != "" {
		lines = append(lines, st.ModalTitle.Render(text.Truncate(o.Title, inner)))
	}

	for i, it := range o.Items {
		if it.Separator {
			lines = append(lines, st.Divider.Render(strings.Repeat(t.Glyphs.Divider, inner)))
			continue
		}
		lines = append(lines, renderMenuItem(o, i, it, inner, st))
	}

	style := st.Menu
	if o.Danger {
		style = style.BorderForeground(st.Error.GetForeground())
	}
	return style.Render(strings.Join(lines, "\n"))
}

// renderMenuItem draws one row, including its right-aligned hint.
func renderMenuItem(o Overlay, i int, it MenuItem, inner int, st theme.Styles) string {
	label := it.Label
	switch {
	case it.Danger:
		label = st.Error.Render(label)
	case it.Disabled:
		label = st.Muted.Render(label)
	}

	row := text.PadRight(label, inner)

	if it.Hint != "" {
		// The hint is right-aligned, so the label is truncated to what is left.
		avail := inner - text.VisibleWidth(it.Hint) - 1
		row = text.PadRight(text.Truncate(it.Label, max(avail, 0)), max(avail, 0)) +
			" " + st.StatusLabel.Render(it.Hint)
	}

	if i == o.Cursor && !it.Disabled {
		return st.MenuItemSelected.Render(row)
	}
	return st.MenuItem.Render(row)
}

func viewModal(o Overlay, width int, t theme.Theme) string {
	st := t.Styles

	inner := min(width-8, MaxWidth-4)
	if inner < 10 {
		inner = max(width, 10)
	}

	var lines []string
	if o.Title != "" {
		lines = append(lines, st.ModalTitle.Render(text.Truncate(o.Title, inner)))
	}
	// Overlay content can come from a contact's "about" text, so it gets the
	// same sanitisation as a message body before it is drawn.
	if o.Content != "" {
		lines = append(lines, text.Wrap(text.Sanitize(o.Content), inner)...)
	}
	for _, l := range o.Body {
		lines = append(lines, text.Wrap(text.Sanitize(l), inner)...)
	}

	if len(o.Buttons) > 0 {
		lines = append(lines, "", renderButtons(o, inner, st))
	}

	return st.Modal.Render(strings.Join(lines, "\n"))
}

// renderButtons draws a modal's button row.
func renderButtons(o Overlay, inner int, st theme.Styles) string {
	parts := make([]string, 0, len(o.Buttons))
	for i, b := range o.Buttons {
		label := " " + text.Truncate(b.Label, 16) + " "

		// The focused button is filled so that the tab order is visible at a
		// glance; an unfocused destructive button keeps its danger colour.
		switch {
		case i == o.Cursor:
			label = st.MenuItemSelected.Render(label)
		case b.Danger:
			label = st.Error.Render(label)
		default:
			label = st.Muted.Render(label)
		}
		parts = append(parts, label)
	}

	return centreLine(strings.Join(parts, "  "), inner)
}

// viewToast renders the notification box.
//
// The width is bounded by the terminal so that a long message wraps rather than
// running off the screen edge.
func viewToast(o Overlay, width int, t theme.Theme) string {
	style := t.Styles.Toast
	if o.Danger {
		style = style.BorderForeground(t.Styles.Error.GetForeground())
	}

	w := min(text.VisibleWidth(o.Content)+4, width-4)
	if w < 10 {
		// Too narrow to be legible; the caller already has the status bar for
		// short messages.
		return ""
	}
	return style.Width(w).Render(text.TruncateStyled(o.Content, w-4))
}

// --- canvas helpers ---

// blankCanvas returns a screen of spaces.
func blankCanvas(width, height int) []string {
	rows := make([]string, height)
	blank := strings.Repeat(" ", width)
	for i := range rows {
		rows[i] = blank
	}
	return rows
}

// centre returns the box's rows padded above and below to exactly height lines.
//
// Content beyond height is dropped rather than pushed off the screen: a dialog
// larger than the terminal is a layout problem, not a reason to corrupt the
// scrollback.
func centre(box string, height int) []string {
	lines := strings.Split(box, "\n")
	if len(lines) >= height {
		return lines[:height]
	}

	top := (height - len(lines)) / 2
	out := make([]string, 0, height)
	for range top {
		out = append(out, "")
	}
	out = append(out, lines...)
	for len(out) < height {
		out = append(out, "")
	}
	return out
}

// bottomAnchored indents the box horizontally, for the caller to place vertically.
func bottomAnchored(box string, width int) []string {
	left := 0
	if w := lipgloss.Width(box); w > 0 {
		left = max((width-w)/2, 0)
	}
	lines := strings.Split(box, "\n")
	for i, l := range lines {
		lines[i] = strings.Repeat(" ", left) + l
	}
	return lines
}

// paint fills the canvas from src rows, clipping each to the canvas width.
//
// The canvas starts blank, so this replaces rather than composes; that is what
// lets the row keep the styling it was rendered with.
func paint(canvas, src []string, height int) string {
	width := 0
	if len(canvas) > 0 {
		width = text.VisibleWidth(canvas[0])
	}
	for i := range height {
		if i >= len(src) {
			break
		}
		if width > 0 {
			canvas[i] = placeRow(src[i], width)
		}
	}
	return strings.Join(canvas, "\n")
}

// fitRow places src over the start of dst, keeping dst's width.
//
// Cell-level composition rather than concatenation, because src carries colour
// escapes and dst is the screen behind it: only the covered cells may change.
func fitRow(src, dst string) string {
	return text.OverlayCells(src, dst)
}

// placeRow puts a box row onto a blank canvas row, padding or clipping to width.
func placeRow(src string, width int) string {
	switch w := text.VisibleWidth(src); {
	case w == width:
		return src
	case w < width:
		return src + strings.Repeat(" ", width-w)
	default:
		return text.TruncateStyled(src, width)
	}
}

// centreLine puts s in the middle of a line exactly width cells wide.
func centreLine(s string, width int) string {
	pad := width - text.VisibleWidth(s)
	if pad <= 0 {
		return s
	}
	left := pad / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", pad-left)
}

// Stack is an ordered set of overlays, innermost last.
type Stack struct {
	items []Overlay
}

// Push adds an overlay.
func (s *Stack) Push(o Overlay) { s.items = append(s.items, o) }

// Pop removes the topmost overlay.
//
// It reports whether anything was removed, so the caller can treat a pop against
// an empty stack as a signal to quit rather than as a silent no-op.
func (s *Stack) Pop() (Overlay, bool) {
	if len(s.items) == 0 {
		return Overlay{}, false
	}
	top := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return top, true
}

// Top returns the topmost overlay.
func (s Stack) Top() (Overlay, bool) {
	if len(s.items) == 0 {
		return Overlay{}, false
	}
	return s.items[len(s.items)-1], true
}

// Len returns the overlay count.
func (s Stack) Len() int { return len(s.items) }

// Empty reports whether no overlay is open.
func (s Stack) Empty() bool { return len(s.items) == 0 }

// All returns a copy of the stack, oldest first.
//
// The copy is what lets a caller inspect the whole stack without being able to mutate
// it: expiry, for instance, has to look at every toast but may only change the stack
// through Pop.
func (s Stack) All() []Overlay {
	out := make([]Overlay, len(s.items))
	copy(out, s.items)
	return out
}

// SetTop replaces the topmost overlay, for menus whose cursor moved.
func (s *Stack) SetTop(o Overlay) {
	if len(s.items) > 0 {
		s.items[len(s.items)-1] = o
	}
}

// View renders the whole stack, innermost last, so that the topmost overlay draws
// over the ones beneath it.
//
// Toasts accumulate along the bottom so that several notifications stack upwards
// instead of covering one another.
func (s Stack) View(width, height int, t theme.Theme) string {
	if len(s.items) == 0 || width <= 0 || height <= 0 {
		return ""
	}

	canvas := blankCanvas(width, height)

	for _, o := range s.items {
		if o.Kind == KindToast {
			continue
		}
		for i, row := range centre(viewBox(o, width, t), height) {
			if i < len(canvas) {
				canvas[i] = fitRow(row, canvas[i])
			}
		}
	}

	bottom := height
	for i := len(s.items) - 1; i >= 0; i-- {
		o := s.items[i]
		if o.Kind != KindToast {
			continue
		}
		box := viewToast(o, width, t)
		if box == "" {
			continue
		}

		rows := bottomAnchored(box, width)
		bottom -= len(rows)
		if bottom < 0 {
			break
		}
		for j, row := range rows {
			if bottom+j < len(canvas) {
				canvas[bottom+j] = fitRow(row, canvas[bottom+j])
			}
		}
	}

	return strings.Join(canvas, "\n")
}
