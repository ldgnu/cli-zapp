package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Styles bundles every lipgloss style the UI renders with.
//
// Every style is derived from the owning [Theme], so a palette change cannot
// leave a style pointing at a colour that no longer exists.
type Styles struct {
	// Sidebar chrome.
	//
	// None of these carry a width. Layout widths belong to the component that
	// knows the pane's size, which is only correct at the moment of drawing.
	Sidebar      lipgloss.Style
	SidebarHead  lipgloss.Style
	SearchBox    lipgloss.Style
	SearchPrompt lipgloss.Style
	Brand        lipgloss.Style

	// Chat list rows.
	ChatRow         lipgloss.Style
	ChatRowSelected lipgloss.Style
	ChatTitle       lipgloss.Style
	ChatTitleUnread lipgloss.Style
	ChatPreview     lipgloss.Style
	ChatTime        lipgloss.Style
	ChatBadge       lipgloss.Style

	// Conversation header.
	Header          lipgloss.Style
	HeaderTitle     lipgloss.Style
	HeaderSubtitle  lipgloss.Style
	HeaderRule      lipgloss.Style
	PresenceOnline  lipgloss.Style
	PresenceOffline lipgloss.Style

	// Message bubbles.
	Outgoing     lipgloss.Style
	Incoming     lipgloss.Style
	BubbleTime   lipgloss.Style
	BubbleFailed lipgloss.Style
	Reaction     lipgloss.Style
	ReplyQuote   lipgloss.Style
	Selection    lipgloss.Style
	SelectionTag lipgloss.Style
	System       lipgloss.Style
	Mention      lipgloss.Style
	SearchHit    lipgloss.Style

	// Input.
	Composer       lipgloss.Style
	Palette        lipgloss.Style
	PalettePrompt  lipgloss.Style
	PaletteItem    lipgloss.Style
	PaletteCursor  lipgloss.Style
	PaletteSection lipgloss.Style

	ComposerFocused lipgloss.Style
	ComposerHint    lipgloss.Style
	ComposerPrompt  lipgloss.Style

	// Chrome.
	StatusBar   lipgloss.Style
	StatusKey   lipgloss.Style
	StatusLabel lipgloss.Style
	Divider     lipgloss.Style
	DividerHot  lipgloss.Style
	Scrollbar   lipgloss.Style
	ScrollThumb lipgloss.Style
	EmptyState  lipgloss.Style

	// Overlays.
	Overlay          lipgloss.Style
	Modal            lipgloss.Style
	ModalTitle       lipgloss.Style
	Menu             lipgloss.Style
	MenuItem         lipgloss.Style
	MenuItemSelected lipgloss.Style
	Toast            lipgloss.Style
	HelpKey          lipgloss.Style
	HelpDesc         lipgloss.Style

	// Semantic.
	Error   lipgloss.Style
	Warning lipgloss.Style
	Success lipgloss.Style
	Accent  lipgloss.Style
	Muted   lipgloss.Style
}

// buildStyles derives every style from a theme's palette and metrics.
func buildStyles(t Theme) Styles {
	p := t.Palette
	m := t.Metrics

	// fg yields a no-op style for a nil colour so that a partially specified
	// palette renders sensibly instead of panicking.
	fg := func(c color.Color) lipgloss.Style {
		if c == nil {
			return lipgloss.NewStyle()
		}
		return lipgloss.NewStyle().Foreground(c)
	}

	// The composer grows vertically with the input, so its width is left unset
	// and applied per frame by the component.
	sidebar := lipgloss.NewStyle().Background(p.SidebarBackground)

	return Styles{
		Sidebar:     sidebar,
		SidebarHead: fg(p.SidebarTitle).Bold(true),
		SearchBox: fg(p.Foreground).
			Border(lipgloss.RoundedBorder()).BorderForeground(p.Border).Padding(0, 1),
		SearchPrompt: fg(p.Accent).Bold(true),
		Brand:        fg(p.Accent).Bold(true),

		// No horizontal padding: the component pads its own content to the pane
		// width, and padding applied here lands after that, making every row two
		// cells too wide.
		ChatRow: lipgloss.NewStyle(),
		// The width is applied by the caller from the pane it is drawing, not baked
		// in here: a fixed width overflows the moment the terminal is resized.
		ChatRowSelected: lipgloss.NewStyle().Foreground(p.SidebarSelectedFG),
		ChatTitle:       fg(p.Foreground),
		ChatTitleUnread: fg(p.Foreground).Bold(true),
		ChatPreview:     fg(p.Muted),
		ChatTime:        fg(p.SidebarTitle),
		ChatBadge: lipgloss.NewStyle().
			Background(p.Accent).Foreground(p.SidebarSelectedFG).Padding(0, 1).Bold(true),

		Header: lipgloss.NewStyle().
			Background(p.HeaderBackground).Foreground(p.HeaderForeground).Height(m.HeaderHeight),
		HeaderTitle:     fg(p.HeaderForeground).Bold(true),
		HeaderSubtitle:  fg(p.Muted),
		HeaderRule:      fg(p.Border),
		PresenceOnline:  fg(p.Online),
		PresenceOffline: fg(p.Offline),

		Outgoing:     lipgloss.NewStyle().Background(p.OutgoingBG).Foreground(p.OutgoingFG).Padding(0, 1),
		Incoming:     lipgloss.NewStyle().Background(p.IncomingBG).Foreground(p.IncomingFG).Padding(0, 1),
		BubbleTime:   fg(p.Muted),
		BubbleFailed: fg(p.Failed),
		Reaction:     fg(p.Accent),
		ReplyQuote:   fg(p.Muted).Italic(true),
		Selection:    lipgloss.NewStyle().Background(p.SidebarSelected),
		SelectionTag: fg(p.Accent).Bold(true),
		System:       fg(p.SystemText).Italic(true),
		Mention:      fg(p.Mention).Bold(true),
		SearchHit:    lipgloss.NewStyle().Background(p.SearchMatch),

		Composer: lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(p.Border),
		ComposerFocused: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).BorderForeground(p.BorderFocus),
		ComposerHint:   fg(p.Faint),
		ComposerPrompt: fg(p.Accent).Bold(true),

		StatusBar:   fg(p.StatusBarFG),
		StatusKey:   fg(p.Accent).Bold(true),
		StatusLabel: fg(p.Faint),
		Divider:     fg(p.Border),
		DividerHot:  fg(p.BorderFocus),
		Scrollbar:   fg(p.Faint),
		ScrollThumb: fg(p.BorderFocus),
		EmptyState:  fg(p.Muted),

		Overlay: lipgloss.NewStyle(),
		Modal: lipgloss.NewStyle().Background(p.ModalBackground).Foreground(p.Foreground).
			Border(lipgloss.RoundedBorder()).BorderForeground(p.ModalBorder).Padding(1, 2),
		ModalTitle: fg(p.HeaderForeground).Bold(true),
		Menu: lipgloss.NewStyle().Background(p.ModalBackground).Foreground(p.Foreground).
			Border(lipgloss.RoundedBorder()).BorderForeground(p.ModalBorder).Padding(0, 1),
		MenuItem: fg(p.Foreground).PaddingLeft(2).PaddingRight(2),
		MenuItemSelected: lipgloss.NewStyle().
			Background(p.Accent).Foreground(p.Foreground).
			PaddingLeft(2).PaddingRight(2).Bold(true),
		Toast: lipgloss.NewStyle().Background(p.ModalBackground).Foreground(p.Foreground).
			Border(lipgloss.RoundedBorder()).BorderForeground(p.ModalBorder).Padding(0, 2),
		HelpKey:  fg(p.Accent).Bold(true),
		HelpDesc: fg(p.Foreground),

		Error:   fg(p.Failed),
		Warning: fg(p.Delivered),
		Success: fg(p.Online),
		Accent:  fg(p.Accent),
		Muted:   fg(p.Muted),
	}
}
