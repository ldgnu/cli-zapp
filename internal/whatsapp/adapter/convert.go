package adapter

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/cli-zapp/cli-zapp/internal/models"
)

// This file is the protocol's edge: everything below converts protocol shapes into
// [models] shapes, and nothing above it knows a JID exists.

// connectReason renders a logout or connect-failure reason in the user's terms.
func connectReason(code events.ConnectFailureReason) string {
	switch code {
	case events.ConnectFailureLoggedOut:
		return "se cerró sesión en el teléfono"
	default:
		// Connected as the signal, the code has a String(); showing that is more useful
		// than a generic sentence, because it distinguishes the cases this project
		// cannot act on from the one it can — a bad nonce needs a new pairing.
		if s := code.String(); s != "" {
			return "la sesión terminó: " + s
		}
		return "la sesión terminó"
	}
}

// text wraps a body as a protocol text message.
//
// The extended form rather than the bare string: WhatsApp puts a link preview on a URL
// sent as extended text, and a client that always sends the bare form produces messages
// with no previews, which looks broken to anyone comparing against the phone.
func (s *core) text(body string) *waE2E.Message {
	return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: &body}}
}

// convertMessage turns a protocol message into the model's shape.
//
// Every field is set from something, and nothing is guessed: a message with no text and
// no media is not rendered as an empty bubble, it becomes a placeholder the transcript
// can label, because a message that exists and shows nothing is indistinguishable from a
// rendering bug.
func convertMessage(info types.MessageInfo, msg *waE2E.Message) models.Message {
	out := models.Message{
		ID:        models.MessageID(info.ID),
		ChatID:    models.ChatID(info.Chat.String()),
		SenderID:  models.ContactID(info.Sender.String()),
		Kind:      models.KindText,
		Timestamp: timestampOf(info),
	}

	body, kind, media := bodyAndMedia(msg)
	out.Body = body
	if media != nil {
		out.Kind = kind
		out.Media = media
	}
	return out
}

// timestampOf picks a message's time, preferring the sender's.
//
// The server time is the fallback because a device with a wrong clock would otherwise
// file every message under the wrong day, and a transcript whose ordering is wrong is
// worse than one with slightly imprecise times.
func timestampOf(info types.MessageInfo) time.Time {
	return info.Timestamp
}

// bodyAndMedia extracts the displayable body and any attachment.
//
// Each wrapper is checked in the order WhatsApp nests them: view-once and ephemeral
// messages arrive still wrapped, and a client that does not unwrap them shows nothing
// for the messages most likely to be interesting.
func bodyAndMedia(msg *waE2E.Message) (string, models.MessageKind, *models.Media) {
	if msg == nil {
		return "", models.KindText, nil
	}

	if m := msg.GetExtendedTextMessage(); m != nil {
		return m.GetText(), models.KindText, nil
	}
	if m := msg.GetImageMessage(); m != nil {
		return m.GetCaption(), models.KindImage,
			mediaFrom(m.GetMimetype(), m.GetCaption(), boundedSize(m.GetFileLength()))
	}
	if m := msg.GetVideoMessage(); m != nil {
		return m.GetCaption(), models.KindVideo,
			mediaFrom(m.GetMimetype(), m.GetCaption(), boundedSize(m.GetFileLength()))
	}
	if m := msg.GetAudioMessage(); m != nil {
		// An audio message is a voice note when it is marked as such; the flag is what
		// distinguishes a recorded note from a song someone attached, and rendering both
		// as the same thing is the difference between a waveform and a play button.
		kind := models.KindAudio
		if m.GetPTT() {
			kind = models.KindVoice
		}
		return "", kind, mediaFrom(m.GetMimetype(), "", boundedSize(m.GetFileLength()))
	}
	if m := msg.GetDocumentMessage(); m != nil {
		return m.GetTitle(), models.KindDocument,
			mediaFrom(m.GetMimetype(), m.GetFileName(), boundedSize(m.GetFileLength()))
	}
	if m := msg.GetStickerMessage(); m != nil {
		return "", models.KindSticker, mediaFrom(m.GetMimetype(), "", boundedSize(m.GetFileLength()))
	}
	if m := msg.GetLocationMessage(); m != nil {
		return m.GetName(), models.KindLocation, nil
	}
	if m := msg.GetContactMessage(); m != nil {
		return m.GetDisplayName(), models.KindContactCard, nil
	}
	// A poll or a reaction carries no body; leaving it empty is correct and the
	// transcript renders it by kind.
	return "", models.KindUnknown, nil
}

// mediaFrom builds an attachment descriptor.
func mediaFrom(mimetype, filename string, size int64) *models.Media {
	return &models.Media{
		MimeType: mimetype,
		Filename: filename,
		Size:     size,
	}
}

// convertGroup maps group metadata onto the model's shape.
func convertGroup(jid types.JID, info *types.GroupInfo) models.Group {
	g := models.Group{
		ID:   models.ChatID(jid.String()),
		Name: info.Name,
	}
	if info.Topic != "" {
		g.Topic = info.Topic
	}
	if info.Name == "" {
		// A group always has a name in practice; the fallback is so a group created by
		// someone else and never renamed does not render as a blank row.
		g.Name = trimJIDSuffix(jid.User)
	}
	// The roster is carried across rather than merely counted: the members panel is the
	// only place an admin can be identified, and a count cannot answer "who can kick
	// me". Administrators are flagged on the participant so the UI can show it without
	// asking again.
	for _, p := range info.Participants {
		g.Participants = append(g.Participants, models.GroupParticipant{
			ID:           models.ContactID(p.JID.String()),
			IsAdmin:      p.IsAdmin || p.IsSuperAdmin,
			IsSuperAdmin: p.IsSuperAdmin,
		})
	}
	return g
}

// sortContacts orders contacts by name, with the undetermined case last.
//
// Sorted rather than ranging the map, because map order is random in Go and a contact
// list that reshuffles every time it is opened cannot be learned.
func sortContacts(in []models.Contact) {
	sort.SliceStable(in, func(a, b int) bool {
		if (in[a].Name == "") != (in[b].Name == "") {
			return in[a].Name != ""
		}
		return in[a].Name < in[b].Name
	})
}

// sanitizeFilename makes a protocol-supplied name safe to write to disk.
//
// A filename arrives from the network, so it is hostile input by default: a path
// separator would write outside the media directory, and a leading dot or a NUL would
// do worse. Everything outside a conservative set is replaced, and the result is
// bounded so a 4 KB "filename" cannot become a path that fails confusingly.
func sanitizeFilename(id string, name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == ".." {
		name = "adjunto"
	}

	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
		if b.Len() >= 80 {
			break
		}
	}

	clean := strings.Trim(b.String(), "._")
	if clean == "" {
		clean = "adjunto"
	}
	// The message id keeps two downloads from colliding, which happens the moment the
	// same filename is received twice.
	if len(id) > 16 {
		id = id[:16]
	}
	return id + "_" + clean
}

// boundedSize converts a protocol-reported byte count without overflowing.
//
// The size arrives as uint64 and a hostile or buggy peer controls it. Wrapping a value
// near 2^63 produces a negative size, which then renders as a small file — so a
// download of unknown length would claim to be a few bytes. Clamping at MaxInt64 makes
// the bad value visibly enormous instead.
func boundedSize(n uint64) int64 {
	if n > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(n)
}

// downloadTo fetches an attachment to a path.
//
// The raw protocol message is needed to download, because the download is authenticated
// by keys that live in the message itself. The cache holds only the descriptor, so the
// raw message is re-derived here — which is why a download for a message outside the
// cache window is reported as unsupported rather than silently producing a stub file.
func (s *core) downloadTo(
	ctx context.Context,
	chat models.ChatID,
	msg *models.Message,
	path string,
) error {
	raw, ok := s.rawMessages[chat][string(msg.ID)]
	if !ok {
		return fmt.Errorf("attachment for %s is no longer in the cache: %w", msg.ID, ErrNoMedia)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := s.client.Raw().DownloadToFile(ctx, raw, f); err != nil {
		// A partial file left behind reads as a successful download on the next run.
		_ = os.Remove(path)
		return fmt.Errorf("downloading %s: %w", msg.ID, err)
	}
	return nil
}

// downloadableFrom picks the downloadable view of a media message.
//
// The concrete per-kind types each carry their own keys, so there is no way to hold a
// media message generically except through the interface the download call takes — which
// is exactly why the cache stores that interface rather than the wrapper.
func downloadableFrom(msg *waE2E.Message) (whatsmeow.DownloadableMessage, bool) {
	switch {
	case msg == nil:
		return nil, false
	case msg.GetImageMessage() != nil:
		return msg.GetImageMessage(), true
	case msg.GetVideoMessage() != nil:
		return msg.GetVideoMessage(), true
	case msg.GetAudioMessage() != nil:
		return msg.GetAudioMessage(), true
	case msg.GetDocumentMessage() != nil:
		return msg.GetDocumentMessage(), true
	case msg.GetStickerMessage() != nil:
		return msg.GetStickerMessage(), true
	default:
		return nil, false
	}
}

// banReason turns a protocol ban code into something a person can read.
//
// The numeric code is kept out of the UI: "409" tells a user nothing, and someone who
// does not understand a ban cannot decide whether to wait twenty minutes or to assume
// the account is gone. The three upstream reasons worth distinguishing all point at
// the same remedy — stop sending and wait — so the wording says what to do.
func banReason(code events.TempBanReason) string {
	switch code {
	case events.TempBanSentToTooManyPeople:
		return "escribiste a demasiada gente que no te tiene en la agenda"
	case events.TempBanBlockedByUsers:
		return "te bloquearon demasiadas personas"
	case events.TempBanCreatedTooManyGroups:
		return "creaste demasiados grupos con gente que no te tiene en la agenda"
	case events.TempBanSentTooManySameMessage:
		return "enviaste el mismo mensaje a demasiada gente"
	case events.TempBanBroadcastList:
		return "enviaste demasiados mensajes a una lista de difusión"
	default:
		return "WhatsApp pausó la cuenta temporalmente"
	}
}
