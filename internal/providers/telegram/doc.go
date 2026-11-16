// Package telegram adapts go-telegram/bot for native messages, bot management,
// metadata and cleanup. Each client represents one bot; callers supply authorized
// destinations and persist bot-scoped mappings. The adapter validates receipts
// and classifies uncertain mutations without automatically replaying them.
package telegram
