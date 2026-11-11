package bots

import (
	"context"
	"net/http"

	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/providers"
	"github.com/veryshyjelly/bifrost/internal/providers/telegram"
	"github.com/veryshyjelly/bifrost/internal/secrets"
)

type CredentialStore interface {
	Credential(context.Context, string, string) (Credential, bool, error)
}

// Pool resolves the current credential on each request, so rotation and import
// need no process-local cache invalidation. Construction performs no API calls.
type Pool struct {
	store                       CredentialStore
	cipher                      *secrets.Cipher
	account, localURL, cloudURL string
	transport                   http.RoundTripper
}

func NewPool(store CredentialStore, key [32]byte, account, localURL, cloudURL string, transport http.RoundTripper) (*Pool, error) {
	if store == nil || account == "" || !providers.ValidURL(localURL) || !providers.ValidURL(cloudURL) {
		return nil, delivery.ErrInvalid
	}
	cipher, err := secrets.New(key)
	if err != nil {
		return nil, err
	}
	return &Pool{store: store, cipher: cipher, account: account, localURL: localURL, cloudURL: cloudURL, transport: transport}, nil
}
func (p *Pool) client(ctx context.Context, account, id, url string) (*telegram.Client, error) {
	if account != p.account || id == "" {
		return nil, delivery.ErrInvalid
	}
	credential, found, err := p.store.Credential(ctx, account, id)
	if err != nil {
		return nil, local(err)
	}
	if !found {
		return nil, delivery.ErrInvalid
	}
	token, err := p.cipher.Open(credential.Ciphertext)
	if err != nil || !ValidToken(token, credential.Identity.ID) {
		return nil, secrets.ErrCiphertext
	}
	return telegram.New(url, token, p.transport)
}
func (p *Pool) Local(ctx context.Context, account, id string) (*telegram.Client, error) {
	return p.client(ctx, account, id, p.localURL)
}
func (p *Pool) Cloud(ctx context.Context, account, id string) (*telegram.Client, error) {
	return p.client(ctx, account, id, p.cloudURL)
}
