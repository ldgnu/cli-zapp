package adapter

import (
	"github.com/cli-zapp/cli-zapp/internal/whatsapp"
)

// The six service types.
//
// # Why six, and not one
//
// The interfaces collide on names. ChatService, GroupService and ContactService each
// declare List and Get; ChatService.Delete takes a chat, MessageService.Delete takes a
// message. Go forbids two methods of the same name on one type, so no single type can
// satisfy the set — and [whatsapp.Services] has named fields for exactly that reason.
//
// Each type embeds the shared core, so the caches, the lock and the event channel are
// one thing rather than six copies. Compile-time assertions live here, next to the
// definitions, so a missing method is reported against the type that should have it
// rather than at the call site in main.
type (
	chatSvc    struct{ *core }
	messageSvc struct{ *core }
	contactSvc struct{ *core }
	groupSvc   struct{ *core }
	mediaSvc   struct{ *core }
	syncSvc    struct{ *core }
)

var (
	_ whatsapp.ChatService    = (*chatSvc)(nil)
	_ whatsapp.MessageService = (*messageSvc)(nil)
	_ whatsapp.ContactService = (*contactSvc)(nil)
	_ whatsapp.GroupService   = (*groupSvc)(nil)
	_ whatsapp.MediaService   = (*mediaSvc)(nil)
	_ whatsapp.SyncService    = (*syncSvc)(nil)
)

// NewServices builds the service bundle over a connected client.
//
// It does not connect. Connecting is [Client.Connect], and the UI must be able to
// render a pairing view in between.
func NewServices(c *Client) whatsapp.Services {
	base := newCore(c)
	return whatsapp.Services{
		Chat:    &chatSvc{base},
		Message: &messageSvc{base},
		Contact: &contactSvc{base},
		Group:   &groupSvc{base},
		Media:   &mediaSvc{base},
		Sync:    &syncSvc{base},
	}
}
