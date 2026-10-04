package keybindings

// Defaults returns cli-zapp's standard bindings.
//
// The layout follows the i3wm vocabulary the application is designed around:
// "mod" means ctrl, hjkl navigate, and the modifier-free keys stay free for
// scrolling and text entry.
//
// # Why these are data and not code
//
// Every entry here is overridable from configuration. Defaults live in one
// place precisely so that they *can* be replaced wholesale: a user who wants
// emacs navigation supplies a different table rather than patching behaviour
// out of the components.
//
// # Reserved keys
//
// These deliberately have no default binding, because the components need them
// or they are handled outside the binding system:
//
//   - printable runes while the composer has focus: consumed as text
//   - enter while the composer has focus: send the message
//   - esc: handled directly, since it must always abandon the current mode
func Defaults() []Binding {
	return []Binding{
		// Global.
		{
			Action: ActionQuit, Global: true,
			Keys: []Key{key("mod+q")}, Help: "Salir, o cerrar el diálogo abierto",
			Short: "salir",
		},
		{
			Action: ActionCancel, Global: true,
			Keys: []Key{key(KeyEscape)}, Help: "Cancelar la acción actual",
		},
		{
			Action: ActionSearch, Global: true,
			Keys: []Key{key("mod+f")}, Help: "Buscar conversaciones y mensajes",
			Short: "buscar",
		},
		{
			Action: ActionSync, Global: true,
			Keys: []Key{key("mod+r")}, Help: "Reconectar y sincronizar",
		},
		{
			Action: ActionNewChat, Global: true,
			Keys: []Key{key("mod+n")}, Help: "Abrir el menú de conversaciones",
			Short: "chats",
		},
		{
			Action: ActionInfo, Global: true,
			Keys: []Key{key("mod+g")}, Help: "Información del contacto o del grupo",
			Short: "info",
		},
		{
			Action: ActionHelp, Global: true,
			Keys: []Key{key("mod+?")}, Help: "Mostrar esta ayuda",
			Short: "ayuda",
		},
		{
			// mod+enter is the primary action, per the i3wm convention where
			// "enter" is bound to the focused container's default action.
			Action: ActionConfirm, Global: true,
			Keys: []Key{key("mod+enter")}, Help: "Acción principal del panel enfocado",
		},
		{
			// mod+p rather than mod+k. mod+k is "up" in the i3/vim dialect this
			// interface follows, and that convention has priority here: a key
			// that means one thing in every other pane of a keyboard-driven
			// application must not mean another thing in one of them. mod+p is
			// the other established spelling for a command palette, so nothing
			// is lost by choosing it.
			Action: ActionPalette, Global: true,
			Keys: []Key{key("mod+shift+p")}, Help: "Abrir la paleta de comandos",
			Short: "comandos",
		},
		{
			Action: ActionSelectAll, Global: true,
			Keys: []Key{key("mod+a")}, Help: "Seleccionar todos los mensajes",
			Short: "todo",
		},
		{
			// Tab / Shift+Tab switch panels. They are bound globally because a
			// panel switch is meaningful from anywhere, and are also listed as
			// navigation below so the help groups them sensibly.
			Action: PanelNext, Global: true,
			Keys: []Key{key(KeyTab)}, Help: "Ir al panel siguiente",
		},
		{
			Action: PanelPrev, Global: true,
			Keys: []Key{key("shift+" + KeyTab)}, Help: "Ir al panel anterior",
		},

		// Sidebar.
		{
			Action: NavUp, Panel: PanelSidebar,
			Keys: []Key{key("mod+k"), key(KeyUp)},
			Help: "Chat anterior",
		},
		{
			Action: NavDown, Panel: PanelSidebar,
			Keys: []Key{key("mod+j"), key(KeyDown)},
			Help: "Chat siguiente",
		},
		{
			Action: ChatOpen, Panel: PanelSidebar,
			Keys: []Key{key(KeyEnter)}, Help: "Abrir la conversación seleccionada",
			Short: "abrir",
		},
		{
			Action: ActionToggleRead, Panel: PanelSidebar,
			Keys: []Key{key("mod+u")}, Help: "Marcar la conversación como leída o no leída",
			Short: "leída",
		},
		{
			Action: ActionTogglePin, Panel: PanelSidebar,
			Keys: []Key{key("mod+p")}, Help: "Anclar o desanclar la conversación",
			Short: "anclar",
		},
		{
			Action: ActionToggleMute, Panel: PanelSidebar,
			Keys: []Key{key("mod+m")}, Help: "Silenciar o quitar el silencio",
			Short: "silenciar",
		},
		{
			Action: ActionToggleArchive, Panel: PanelSidebar,
			Keys: []Key{key("mod+e")}, Help: "Archivar o desarchivar la conversación",
		},
		{
			Action: ActionDeleteChat, Panel: PanelSidebar,
			Keys: []Key{key("mod+backspace")}, Help: "Eliminar la conversación",
		},

		// Message list.
		{
			Action: NavUp, Panel: PanelMessages,
			Keys: []Key{key("mod+k"), key(KeyUp)},
			Help: "Mensaje anterior",
		},
		{
			Action: NavDown, Panel: PanelMessages,
			Keys: []Key{key("mod+j"), key(KeyDown)},
			Help: "Mensaje siguiente",
		},
		{
			// Panel focus is bound globally rather than per panel. The point of an
			// i3-style hierarchy is that mod+h reaches the sidebar from wherever the
			// user is; scoping it to the message list would leave the key silently
			// doing nothing from the composer, which is where typing usually happens.
			Action: NavLeft, Global: true,
			Keys: []Key{key("mod+h")},
			Help: "Ir a la lista de conversaciones",
		},
		{
			Action: NavRight, Global: true,
			Keys: []Key{key("mod+l")},
			Help: "Ir al campo de escritura",
		},
		{
			// A bare page key scrolls a page, and ctrl+b / alt+pgup is the dedicated
			// "page" action. Both are offered because a user who thinks of PageUp as
			// a page should not have to discover the distinction.
			Action: ScrollUp, Panel: PanelMessages,
			Keys: []Key{key("k"), key("ctrl+u"), key(KeyPageUp), key("shift+" + KeyUp)},
			Help: "Desplazar hacia arriba",
		},
		{
			// Half-page scrolling is ctrl+u / ctrl+y. The symmetric ctrl+d and ctrl+e
			// pair would be the obvious choice, but ctrl+d deletes the selected message
			// and ctrl+e edits it — both bound in this same panel, where a global is not
			// involved and the two would simply shadow each other. ctrl+y is the pair a
			// terminal user already has in their fingers for the other direction.
			Action: ScrollDown, Panel: PanelMessages,
			Keys: []Key{key("j"), key("ctrl+y"), key(KeyPageDown), key("shift+" + KeyDown)},
			Help: "Desplazar hacia abajo",
		},
		{
			Action: ScrollTop, Panel: PanelMessages,
			Keys: []Key{key("ctrl+home"), key("g")}, Help: "Ir al mensaje más antiguo",
		},
		{
			Action: ScrollBottom, Panel: PanelMessages,
			Keys: []Key{key("ctrl+end"), key("G")}, Help: "Ir al mensaje más reciente",
		},
		{
			Action: PageUp, Panel: PanelMessages,
			Keys: []Key{key("ctrl+b"), key("alt+" + KeyPageUp)}, Help: "Desplazar hacia arriba una página",
		},
		{
			Action: PageDown, Panel: PanelMessages,
			Keys: []Key{key("alt+" + KeyPageDown)}, Help: "Desplazar hacia abajo una página",
		},
		{
			Action: ActionReply, Panel: PanelMessages,
			Keys: []Key{key("r")}, Help: "Responder al mensaje seleccionado",
			Short: "responder",
		},
		{
			Action: ActionEdit, Panel: PanelMessages,
			Keys: []Key{key("mod+e")}, Help: "Editar el mensaje seleccionado",
		},
		{
			Action: ActionDelete, Panel: PanelMessages,
			Keys: []Key{key("mod+d")}, Help: "Eliminar el mensaje seleccionado",
		},
		{
			Action: ActionReact, Panel: PanelMessages,
			Keys: []Key{key("R")}, Help: "Reaccionar al mensaje seleccionado",
			Short: "reaccionar",
		},
		{
			Action: ActionForward, Panel: PanelMessages,
			Keys: []Key{key("mod+shift+f")}, Help: "Reenviar el mensaje seleccionado",
		},
		{
			Action: ActionCopy, Panel: PanelMessages,
			Keys: []Key{key("mod+c")}, Help: "Copiar el mensaje seleccionado",
			Short: "copiar",
		},
		{
			Action: ActionSelect, Panel: PanelMessages,
			Keys: []Key{key("space")}, Help: "Seleccionar o deseleccionar el mensaje",
			Short: "seleccionar",
		},
		{
			Action: ActionDownload, Panel: PanelMessages,
			Keys: []Key{key("mod+s")}, Help: "Descargar el adjunto del mensaje",
		},
		{
			Action: ActionOpen, Panel: PanelMessages,
			Keys: []Key{key("mod+o")}, Help: "Abrir el adjunto fuera del terminal",
		},

		// Composer.
		{
			// ctrl+n is reserved for starting a new chat, so switching chats from
			// the composer uses alt+arrow, leaving the plain arrows available for
			// moving the caret in multi-line text.
			Action: NavUp, Panel: PanelComposer,
			Keys: []Key{key("alt+" + KeyUp)},
			Help: "Chat anterior",
		},
		{
			Action: NavDown, Panel: PanelComposer,
			Keys: []Key{key("alt+" + KeyDown)},
			Help: "Chat siguiente",
		},
		{
			Action: ScrollUp, Panel: PanelComposer,
			Keys: []Key{key("alt+k"), key("alt+" + KeyPageUp)}, Help: "Desplazar hacia arriba",
		},
		{
			Action: ScrollDown, Panel: PanelComposer,
			Keys: []Key{key("alt+j"), key("alt+" + KeyPageDown)}, Help: "Desplazar hacia abajo",
		},
		{
			Action: SendMessage, Panel: PanelComposer,
			Keys: []Key{key(KeyEnter)}, Help: "Enviar el mensaje",
			Short: "enviar",
		},
		{
			Action: InsertNewline, Panel: PanelComposer,
			Keys: []Key{key("alt+enter"), key("shift+enter")}, Help: "Insertar un salto de línea",
			Short: "nueva línea",
		},
	}
}

// Navigation and panel actions, defined separately from the application actions
// above because they are structural rather than domain-specific.
const (
	NavUp    Action = "nav.up"
	NavDown  Action = "nav.down"
	NavLeft  Action = "nav.left"
	NavRight Action = "nav.right"

	ScrollUp     Action = "scroll.up"
	ScrollDown   Action = "scroll.down"
	ScrollTop    Action = "scroll.top"
	ScrollBottom Action = "scroll.bottom"
	PageUp       Action = "scroll.page_up"
	PageDown     Action = "scroll.page_down"

	PanelNext Action = "panel.next"
	PanelPrev Action = "panel.prev"

	SendMessage   Action = "composer.send"
	InsertNewline Action = "composer.newline"

	ChatOpen Action = "sidebar.open"
)

// key is a helper for declaring defaults.
//
// Panicking here is deliberate: a malformed default is a programming error
// caught by the package's own tests at startup, never a runtime condition a
// user could trigger.
func key(spec string) Key {
	k, err := ParseKey(spec)
	if err != nil {
		panic("keybindings: invalid default binding " + spec + ": " + err.Error())
	}
	return k
}

// DefaultMap returns the default binding map, validated.
//
// It panics if the defaults conflict, which makes a bad default a failing test
// rather than a broken installation.
func DefaultMap() Map {
	m := NewMap(Defaults())
	if err := m.Check(); err != nil {
		panic(err)
	}
	return m
}
