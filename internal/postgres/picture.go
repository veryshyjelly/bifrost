package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/metadata"
	"github.com/veryshyjelly/bifrost/internal/postgres/db"
)

var pictureHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

func metadataPicturePlan(ctx context.Context, q *db.Queries, row db.Job) (metadata.PicturePlan, bool, error) {
	raw, err := q.MetadataPicturePlan(ctx, db.MetadataPicturePlanParams{AccountID: row.AccountID, JobID: row.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return metadata.PicturePlan{}, false, nil
	}
	if err != nil {
		return metadata.PicturePlan{}, false, err
	}
	var p metadata.PicturePlan
	if json.Unmarshal(raw, &p) != nil || p.JobID != row.ID || p.Key != row.Dedup {
		return p, false, delivery.ErrInvalid
	}
	return p, true, nil
}
func (s *Store) MetadataPicturePlan(ctx context.Context, j delivery.Job) (metadata.PicturePlan, bool, error) {
	var p metadata.PicturePlan
	var found bool
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		var err error
		p, found, err = metadataPicturePlan(ctx, q, row)
		return err
	})
	return p, found, err
}

func validPictureTarget(text metadata.Plan, p metadata.PicturePlan) bool {
	if !text.TextComplete || p.Key != text.Key || p.JobID != text.JobID || p.Kind != text.Snapshot.Scope || !reflect.DeepEqual(p.Target, text.Target) {
		return false
	}
	if p.Kind == "group" {
		r := p.Target.Route
		return r != nil && p.BotID == "master" && p.ChatID == r.ChatID && p.ChatID < 0 && (p.ChatID == p.Target.Management || !r.Retired && r.ThreadID == 0)
	}
	if p.Kind != "contact" || p.BotID != p.Target.BotID {
		return false
	}
	if p.BotID == "" {
		return p.ChatID == 0
	}
	return strconv.FormatInt(p.ChatID, 10) == p.BotID && p.ChatID > 0
}

func pictureScope(ctx context.Context, q *db.Queries, row db.Job, p metadata.PicturePlan) error {
	text, found, err := metadataPlan(ctx, q, row)
	if err != nil {
		return err
	}
	if !found || !validPictureTarget(text, p) {
		return delivery.ErrInvalid
	}
	current, err := metadataCurrent(ctx, q, row, text)
	if err != nil {
		return err
	}
	if !current {
		return metadata.ErrTarget
	}
	return nil
}

func (s *Store) MetadataPictureCache(ctx context.Context, j delivery.Job, p metadata.PicturePlan) (metadata.PictureCache, bool, error) {
	var cache metadata.PictureCache
	found := false
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if err := pictureScope(ctx, q, row, p); err != nil {
			return err
		}
		r, err := q.MetadataPictureCache(ctx, db.MetadataPictureCacheParams{AccountID: row.AccountID, ScopeKey: p.CacheKey()})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		cache = metadata.PictureCache{SourceHash: r.SourceHash, PhotoID: r.PhotoID}
		found = true
		return nil
	})
	return cache, found, err
}

func (s *Store) PrepareMetadataPicture(ctx context.Context, j delivery.Job, p metadata.PicturePlan, asset []byte) (metadata.PicturePlan, error) {
	var result metadata.PicturePlan
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		if old, found, err := metadataPicturePlan(ctx, q, row); err != nil || found {
			result = old
			return err
		}
		if err := pictureScope(ctx, q, row, p); err != nil {
			return err
		}
		if p.Complete {
			return delivery.ErrInvalid
		}
		unavailable := p.Kind == "group" && p.ChatID == p.Target.Management || p.Kind == "contact" && p.BotID == ""
		if p.Skip == "" {
			if unavailable || !pictureHash.MatchString(p.SourceHash) || !pictureHash.MatchString(p.AssetHash) || metadata.PictureHash(asset) != p.AssetHash || len(asset) < 2 || asset[0] != 0xff || asset[1] != 0xd8 || int64(len(asset)) > metadata.MaxPictureBytes {
				return delivery.ErrInvalid
			}
			if err := q.CreateMetadataPictureAsset(ctx, db.CreateMetadataPictureAssetParams{AccountID: row.AccountID, AssetHash: p.AssetHash, Jpeg: asset}); err != nil {
				return err
			}
		} else {
			if len(asset) != 0 || p.AssetHash != "" {
				return delivery.ErrInvalid
			}
			switch p.Skip {
			case "inbox":
				if p.Kind != "group" || p.ChatID != p.Target.Management {
					return delivery.ErrInvalid
				}
			case "unbound":
				if p.Kind != "contact" || p.BotID != "" {
					return delivery.ErrInvalid
				}
			case "permissions":
				if unavailable || p.Kind != "group" {
					return delivery.ErrInvalid
				}
			case "unavailable":
				if unavailable {
					return delivery.ErrInvalid
				}
			case "cached":
				if unavailable || !pictureHash.MatchString(p.SourceHash) || p.PreviousID == "" {
					return delivery.ErrInvalid
				}
				cache, err := q.MetadataPictureCache(ctx, db.MetadataPictureCacheParams{AccountID: row.AccountID, ScopeKey: p.CacheKey()})
				if err != nil {
					return err
				}
				if cache.SourceHash != p.SourceHash || cache.PhotoID != p.PreviousID {
					return delivery.ErrInvalid
				}
			default:
				return delivery.ErrInvalid
			}
		}
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if err := q.CreateMetadataPicturePlan(ctx, db.CreateMetadataPicturePlanParams{AccountID: row.AccountID, JobID: row.ID, Payload: raw}); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}

func (s *Store) MetadataPictureAsset(ctx context.Context, j delivery.Job) ([]byte, error) {
	var data []byte
	err := s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		p, found, err := metadataPicturePlan(ctx, q, row)
		if err != nil {
			return err
		}
		if !found || p.Skip != "" {
			return delivery.ErrInvalid
		}
		if err := pictureScope(ctx, q, row, p); err != nil {
			return err
		}
		data, err = q.MetadataPictureAsset(ctx, db.MetadataPictureAssetParams{AccountID: row.AccountID, AssetHash: p.AssetHash})
		if err != nil {
			return err
		}
		if metadata.PictureHash(data) != p.AssetHash {
			return delivery.ErrInvalid
		}
		return nil
	})
	return data, err
}

func (s *Store) CompleteMetadataPicture(ctx context.Context, j delivery.Job, photoID string) error {
	return s.withClaim(ctx, j, func(q *db.Queries, row db.Job) error {
		if err := q.LockContactDirectory(ctx, row.AccountID); err != nil {
			return err
		}
		p, found, err := metadataPicturePlan(ctx, q, row)
		if err != nil {
			return err
		}
		if !found {
			return delivery.ErrInvalid
		}
		if p.Complete {
			return nil
		}
		if err := pictureScope(ctx, q, row, p); err != nil {
			return err
		}
		if p.Skip != "" {
			if photoID != "" {
				return delivery.ErrInvalid
			}
		} else {
			if photoID == "" {
				return delivery.ErrIncomplete
			}
			e, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: p.EffectKey()})
			if err != nil {
				return err
			}
			if e.JobID != row.ID || e.State != "done" || !bytes.Equal(bytes.TrimSpace(e.Result), []byte("true")) {
				return delivery.ErrIncomplete
			}
			var request metadata.PictureRequest
			if json.Unmarshal(e.Request, &request) != nil || !reflect.DeepEqual(request, p.Request()) {
				return delivery.ErrInvalid
			}
			observed, err := q.GetEffect(ctx, db.GetEffectParams{AccountID: row.AccountID, EffectKey: p.ObservationKey()})
			if err != nil {
				return err
			}
			if observed.JobID != row.ID || observed.State != "done" {
				return delivery.ErrIncomplete
			}
			var observation struct{ PhotoID string }
			var observedRequest metadata.PictureRequest
			if json.Unmarshal(observed.Request, &observedRequest) != nil || !reflect.DeepEqual(observedRequest, p.Request()) || json.Unmarshal(observed.Result, &observation) != nil || observation.PhotoID != photoID {
				return delivery.ErrInvalid
			}
			if err := q.SaveMetadataPictureCache(ctx, db.SaveMetadataPictureCacheParams{AccountID: row.AccountID, ScopeKey: p.CacheKey(), SourceHash: p.SourceHash, PhotoID: photoID, JobID: row.ID}); err != nil {
				return err
			}
		}
		p.Complete = true
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		return q.SaveMetadataPicturePlan(ctx, db.SaveMetadataPicturePlanParams{AccountID: row.AccountID, JobID: row.ID, Payload: raw})
	})
}

var _ metadata.PictureStore = (*Store)(nil)
