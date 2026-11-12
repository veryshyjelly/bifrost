package postgres

import (
	"context"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

func (s *Store) MessageRevoked(ctx context.Context, account, chat, id string) (bool, error) {
	return s.queries.HistoryMessageRevoked(ctx, db.HistoryMessageRevokedParams{AccountID: account, WhatsappChat: chat, MessageID: id})
}
