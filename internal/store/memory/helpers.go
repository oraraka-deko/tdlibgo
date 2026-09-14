package memory

import (
	"tdlibgo/internal/domain"
	"tdlibgo/internal/store"
	"time"
)

func draftKey(peer domain.Peer, topMessageID int) dialogDraftKey {
	return dialogDraftKey{peerType: peer.Type, peerID: peer.ID, topMessageID: topMessageID}
}

type codeEntry struct {
	code    store.PhoneCode
	expires time.Time
}
