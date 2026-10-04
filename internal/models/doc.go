// Package models defines wterm's internal domain types.
//
// # Layering contract
//
// This package is the vocabulary of the application. It sits at the bottom of
// the dependency graph and imports nothing from wterm. Specifically it must
// never import:
//
//   - internal/whatsapp (or go.mau.fi/whatsmeow) — protocol details
//   - internal/ui (or charm.land/*) — presentation details
//   - internal/storage — persistence details
//
// Identifiers such as [ChatID] and [MessageID] are opaque strings rather than
// protocol types. The WhatsApp JID is one encoding of a ChatID; storing it
// directly would leak the protocol into every layer above. The mapping between
// the two lives exclusively in internal/whatsapp/adapter.
//
// Keeping this package dependency-free is what makes it cheap to test and
// guarantees that the UI cannot accidentally grow protocol knowledge.
package models
