package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/providers"
)

type PicturePlan struct {
	Key                                     string
	JobID                                   int64
	Target                                  Target
	Kind, BotID                             string
	ChatID                                  int64
	SourceHash, AssetHash, PreviousID, Skip string
	Complete                                bool
}
type PictureRequest struct {
	Target                Target
	Kind, BotID           string
	ChatID                int64
	SourceHash, AssetHash string
}

func (p PicturePlan) Request() PictureRequest {
	return PictureRequest{Target: p.Target, Kind: p.Kind, BotID: p.BotID, ChatID: p.ChatID, SourceHash: p.SourceHash, AssetHash: p.AssetHash}
}
func (p PicturePlan) EffectKey() string              { return p.Key + ":metadata-picture" }
func (p PicturePlan) ObservationKey() string         { return p.EffectKey() + ":observed" }
func (p PicturePlan) FrozenRequest() json.RawMessage { b, _ := json.Marshal(p.Request()); return b }
func (p PicturePlan) CacheKey() string {
	generation := ""
	if p.Kind == "group" && p.Target.Route != nil {
		generation = p.Target.Route.ReturnKey
	}
	b, _ := json.Marshal(struct {
		Session, Source, Kind, Bot, Generation string
		Chat                                   int64
	}{p.Target.Session, p.Target.Canonical, p.Kind, p.BotID, generation, p.ChatID})
	return PictureHash(b)
}

type PictureCache struct{ SourceHash, PhotoID string }
type PictureStore interface {
	MetadataPlan(context.Context, delivery.Job) (Plan, bool, error)
	MetadataTextCurrent(context.Context, delivery.Job) (bool, error)
	MetadataPicturePlan(context.Context, delivery.Job) (PicturePlan, bool, error)
	MetadataPictureCache(context.Context, delivery.Job, PicturePlan) (PictureCache, bool, error)
	PrepareMetadataPicture(context.Context, delivery.Job, PicturePlan, []byte) (PicturePlan, error)
	MetadataPictureAsset(context.Context, delivery.Job) ([]byte, error)
	CompleteMetadataPicture(context.Context, delivery.Job, string) error
}
type PictureWhatsApp interface {
	PictureURL(context.Context, string, string) (*string, error)
}
type PictureTelegram interface {
	Me(context.Context) (*models.User, error)
	Chat(context.Context, *bot.GetChatParams) (*models.ChatFullInfo, error)
	ChatMember(context.Context, *bot.GetChatMemberParams) (*models.ChatMember, error)
	ProfilePhotos(context.Context, *bot.GetUserProfilePhotosParams) (*models.UserProfilePhotos, error)
	SetChatPhoto(context.Context, *bot.SetChatPhotoParams) error
	SetMyProfilePhoto(context.Context, *bot.SetMyProfilePhotoParams) error
}
type PictureClients func(context.Context, string, string) (PictureTelegram, error)
type Pictures struct {
	store                PictureStore
	wa                   PictureWhatsApp
	clients              PictureClients
	download             PictureDownloader
	session              string
	management, maxBytes int64
}

func NewPictures(store PictureStore, wa PictureWhatsApp, clients PictureClients, download PictureDownloader, session string, management, maxBytes int64) (*Pictures, error) {
	if store == nil || wa == nil || clients == nil || download == nil || session == "" || management >= 0 || maxBytes <= 0 {
		return nil, delivery.ErrInvalid
	}
	if maxBytes > MaxPictureBytes {
		maxBytes = MaxPictureBytes
	}
	return &Pictures{store: store, wa: wa, clients: clients, download: download, session: session, management: management, maxBytes: maxBytes}, nil
}

func (s *Pictures) current(ctx context.Context, j delivery.Job) error {
	current, err := s.store.MetadataTextCurrent(ctx, j)
	if err != nil {
		return local(err)
	}
	if !current {
		return ErrTarget
	}
	return nil
}

func (s *Pictures) group(ctx context.Context, client PictureTelegram, p PicturePlan) (*models.ChatFullInfo, bool, error) {
	chat, err := client.Chat(ctx, &bot.GetChatParams{ChatID: p.ChatID})
	if err != nil {
		return nil, false, err
	}
	if !PrivateGroup(chat) || chat.ID != p.ChatID {
		return nil, false, ErrTarget
	}
	me, err := client.Me(ctx)
	if err != nil {
		return nil, false, err
	}
	if me == nil || me.ID <= 0 {
		return nil, false, delivery.ErrInvalid
	}
	member, err := client.ChatMember(ctx, &bot.GetChatMemberParams{ChatID: p.ChatID, UserID: me.ID})
	if err != nil {
		return nil, false, err
	}
	allowed := member != nil && member.Type == models.ChatMemberTypeAdministrator && member.Administrator != nil && member.Administrator.CanChangeInfo
	return chat, allowed, nil
}

func (s *Pictures) photoID(ctx context.Context, client PictureTelegram, p PicturePlan) (string, error) {
	if p.Kind == "group" {
		chat, err := client.Chat(ctx, &bot.GetChatParams{ChatID: p.ChatID})
		if err != nil {
			return "", err
		}
		if !PrivateGroup(chat) || chat.ID != p.ChatID {
			return "", ErrTarget
		}
		if chat.Photo == nil {
			return "", nil
		}
		return chat.Photo.BigFileUniqueID, nil
	}
	photos, err := client.ProfilePhotos(ctx, &bot.GetUserProfilePhotosParams{UserID: p.ChatID, Limit: 1})
	if err != nil {
		return "", err
	}
	if photos == nil {
		return "", delivery.ErrInvalid
	}
	if len(photos.Photos) == 0 || len(photos.Photos[0]) == 0 {
		return "", nil
	}
	row := photos.Photos[0]
	return row[len(row)-1].FileUniqueID, nil
}

func (s *Pictures) Prepare(ctx context.Context, j delivery.Job) (PicturePlan, error) {
	if p, found, err := s.store.MetadataPicturePlan(ctx, j); err != nil || found {
		return p, local(err)
	}
	text, found, err := s.store.MetadataPlan(ctx, j)
	if err != nil {
		return PicturePlan{}, local(err)
	}
	if !found || text.Target.Session != s.session || text.Target.Management != s.management {
		return PicturePlan{}, delivery.ErrInvalid
	}
	if !text.TextComplete {
		return PicturePlan{}, delivery.ErrIncomplete
	}
	if err := s.current(ctx, j); err != nil {
		return PicturePlan{}, err
	}
	p := PicturePlan{Key: text.Key, JobID: j.ID, Target: text.Target, Kind: text.Snapshot.Scope, BotID: "master"}
	if p.Kind == "group" {
		if p.Target.Route == nil {
			return p, delivery.ErrInvalid
		}
		p.ChatID = p.Target.Route.ChatID
		if p.ChatID == s.management {
			p.Skip = "inbox"
		}
	} else {
		p.BotID = p.Target.BotID
		if p.BotID == "" {
			p.Skip = "unbound"
		} else {
			p.ChatID, err = strconv.ParseInt(p.BotID, 10, 64)
			if err != nil || p.ChatID <= 0 {
				return p, delivery.ErrInvalid
			}
		}
	}
	if p.Skip != "" {
		p, err = s.store.PrepareMetadataPicture(ctx, j, p, nil)
		return p, local(err)
	}
	client, err := s.clients(ctx, j.AccountID, p.BotID)
	if err != nil {
		return p, err
	}
	if p.Kind == "group" {
		chat, allowed, err := s.group(ctx, client, p)
		if err != nil {
			return p, err
		}
		if !allowed {
			p.Skip = "permissions"
			p, err = s.store.PrepareMetadataPicture(ctx, j, p, nil)
			return p, local(err)
		}
		if chat.Photo != nil {
			p.PreviousID = chat.Photo.BigFileUniqueID
		}
	} else {
		me, err := client.Me(ctx)
		if err != nil {
			return p, err
		}
		if me == nil || me.ID != p.ChatID {
			return p, delivery.ErrInvalid
		}
		p.PreviousID, err = s.photoID(ctx, client, p)
		if err != nil {
			return p, err
		}
	}
	url, err := s.wa.PictureURL(ctx, s.session, p.Target.ID)
	if err != nil {
		return p, err
	}
	if url == nil || *url == "" {
		p.Skip = "unavailable"
		p, err = s.store.PrepareMetadataPicture(ctx, j, p, nil)
		return p, local(err)
	}
	if !PictureURL(*url) {
		return p, &providers.Error{Service: "picture", Status: 400, Code: "INVALID_PICTURE_URL"}
	}
	data, err := s.download.Download(ctx, *url, s.maxBytes)
	if err != nil {
		return p, err
	}
	if int64(len(data)) > s.maxBytes {
		return p, pictureTooLarge()
	}
	p.SourceHash = PictureHash(data)
	cache, found, err := s.store.MetadataPictureCache(ctx, j, p)
	if err != nil {
		return p, local(err)
	}
	if found && cache.SourceHash == p.SourceHash && cache.PhotoID != "" && cache.PhotoID == p.PreviousID {
		p.Skip = "cached"
		p, err = s.store.PrepareMetadataPicture(ctx, j, p, nil)
		return p, local(err)
	}
	jpeg, err := PictureJPEG(data, s.maxBytes)
	if err != nil {
		return p, err
	}
	p.AssetHash = PictureHash(jpeg)
	p, err = s.store.PrepareMetadataPicture(ctx, j, p, jpeg)
	return p, local(err)
}

func (s *Pictures) Apply(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
	p, found, err := s.store.MetadataPicturePlan(ctx, j)
	if err != nil {
		return local(err)
	}
	if !found || p.Target.Session != s.session || p.Target.Management != s.management {
		return delivery.ErrInvalid
	}
	if p.Complete {
		return nil
	}
	if err := s.current(ctx, j); err != nil {
		return err
	}
	if p.Skip != "" {
		return local(s.store.CompleteMetadataPicture(ctx, j, ""))
	}
	if err := e.Permit(); err != nil {
		return err
	}
	client, err := s.clients(ctx, j.AccountID, p.BotID)
	if err != nil {
		return err
	}
	if p.Kind == "group" {
		_, allowed, err := s.group(ctx, client, p)
		if err != nil {
			return err
		}
		if !allowed {
			return errors.Join(delivery.ErrUnsupported, errors.New("Change Group Info permission is required for picture sync"))
		}
	} else {
		me, err := client.Me(ctx)
		if err != nil {
			return err
		}
		if me == nil || me.ID != p.ChatID {
			return delivery.ErrInvalid
		}
	}
	asset, err := s.store.MetadataPictureAsset(ctx, j)
	if err != nil {
		return local(err)
	}
	if PictureHash(asset) != p.AssetHash {
		return delivery.ErrInvalid
	}
	_, err = e.Do(p.EffectKey(), p.FrozenRequest(), func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
		var request PictureRequest
		if json.Unmarshal(raw, &request) != nil {
			return nil, delivery.ErrInvalid
		}
		if request.Kind == "group" {
			err = client.SetChatPhoto(ctx, &bot.SetChatPhotoParams{ChatID: request.ChatID, Photo: &models.InputFileUpload{Filename: "profile.jpg", Data: bytes.NewReader(asset)}})
		} else {
			err = client.SetMyProfilePhoto(ctx, &bot.SetMyProfilePhotoParams{Photo: &models.InputProfilePhotoStatic{Photo: "attach://image", MediaAttachment: bytes.NewReader(asset)}})
		}
		if err != nil {
			return nil, err
		}
		return json.RawMessage(`true`), nil
	})
	if err != nil {
		return err
	}
	observed, err := e.Do(p.ObservationKey(), p.FrozenRequest(), func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		photoID, err := s.photoID(ctx, client, p)
		if err != nil {
			return nil, err
		}
		if photoID == "" {
			return nil, &providers.Error{Service: "telegram", Code: "PICTURE_ID_UNAVAILABLE", Retry: true}
		}
		return json.Marshal(struct{ PhotoID string }{photoID})
	})
	if err != nil {
		return err
	}
	var observation struct{ PhotoID string }
	if json.Unmarshal(observed, &observation) != nil || observation.PhotoID == "" {
		return delivery.ErrInvalid
	}
	return local(s.store.CompleteMetadataPicture(ctx, j, observation.PhotoID))
}
