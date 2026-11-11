package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/veryshyjelly/bifrost/internal/delivery"
	"github.com/veryshyjelly/bifrost/internal/metadata"
	wa "github.com/veryshyjelly/bifrost/internal/providers/openwa"
	tg "github.com/veryshyjelly/bifrost/internal/providers/telegram"
)

type pictureUpload struct {
	Method, Bot, Chat string
	Data              []byte
}
type fixtureCDN func(*http.Request) (*http.Response, error)

func (f fixtureCDN) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type pictureFixture struct {
	*metadataFixture
	pictures                  *metadata.Pictures
	pwa                       *wa.Client
	pclients                  metadata.PictureClients
	download                  *metadata.CDN
	pmu                       sync.Mutex
	memberRoles               map[string]string
	memberErrors              map[string]int
	source                    []byte
	url                       *string
	currentID                 string
	uploads                   []pictureUpload
	urlReads, downloads       int
	failAt, dropAt            int
	public, missingID, forbid bool
	lookupFailure             bool
}

func picturePNG(t *testing.T, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func newPictureFixture(t *testing.T, withMentions ...bool) *pictureFixture {
	t.Helper()
	p := &pictureFixture{metadataFixture: newMetadataFixture(t, withMentions...), source: picturePNG(t, color.NRGBA{R: 255, A: 255}), url: new("https://mmg.whatsapp.net/profile?token=one"), currentID: "old-photo"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			p.pmu.Lock()
			defer p.pmu.Unlock()
			p.urlReads++
			if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/picture") && !strings.HasSuffix(r.URL.Path, "/profile-picture") {
				t.Error("unexpected source mutation", r.Method, r.URL.Path)
			}
			if p.lookupFailure {
				w.WriteHeader(503)
				fmt.Fprint(w, `{"message":"fixture source unavailable"}`)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"url": p.url})
			return
		}
		if strings.Contains(r.Header.Get("Content-Type"), "multipart/") {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
			}
		} else if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		botID := strings.Split(strings.TrimPrefix(r.URL.Path, "/bot"), "/")[0]
		p.pmu.Lock()
		defer p.pmu.Unlock()
		switch method {
		case "getMe":
			id := "999"
			if botID != "master" {
				id = botID
			}
			fmt.Fprintf(w, `{"ok":true,"result":{"id":%s,"is_bot":true,"first_name":"Fixture","username":"fixture_bot"}}`, id)
			return
		case "getChat":
			username := ""
			if p.public {
				username = "public"
			}
			var photo any
			if !p.missingID && p.currentID != "" {
				photo = map[string]string{"small_file_id": "small", "big_file_id": "big", "small_file_unique_id": "small-unique", "big_file_unique_id": p.currentID}
			}
			var chatID json.Number = json.Number(r.FormValue("chat_id"))
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"id": chatID, "type": "supergroup", "username": username, "title": "Fixture", "photo": photo}})
			return
		case "getChatMember":
			user := r.FormValue("user_id")
			role := "administrator"
			if user != "999" {
				role = p.memberRoles[user]
				if role == "" {
					role = "left"
				}
			}
			if code := p.memberErrors[user]; code != 0 {
				fmt.Fprintf(w, `{"ok":false,"error_code":%d,"parameters":{"retry_after":60}}`, code)
				return
			}
			fmt.Fprintf(w, `{"ok":true,"result":{"status":%q,"can_change_info":%t,"is_member":true,"user":{"id":%s,"is_bot":true,"first_name":"Fixture"}}}`, role, !p.forbid, user)
			return
		case "getUserProfilePhotos":
			if p.missingID || p.currentID == "" {
				fmt.Fprint(w, `{"ok":true,"result":{"total_count":0,"photos":[]}}`)
			} else {
				fmt.Fprintf(w, `{"ok":true,"result":{"total_count":1,"photos":[[{"file_id":"file","file_unique_id":%q,"width":640,"height":640}]]}}`, p.currentID)
			}
			return
		case "setChatPhoto", "setMyProfilePhoto":
			field := "photo"
			if method == "setMyProfilePhoto" {
				field = "image"
			}
			file, _, err := r.FormFile(field)
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			defer file.Close()
			data, err := io.ReadAll(file)
			if err != nil {
				t.Error(err)
			}
			if _, format, err := image.DecodeConfig(bytes.NewReader(data)); err != nil || format != "jpeg" {
				t.Error("photo is not a decoded JPEG", format, err)
			}
			p.uploads = append(p.uploads, pictureUpload{Method: method, Bot: botID, Chat: r.FormValue("chat_id"), Data: data})
			n := len(p.uploads)
			if n == p.failAt {
				fmt.Fprint(w, `{"ok":false,"error_code":429,"parameters":{"retry_after":60}}`)
				return
			}
			p.currentID = fmt.Sprintf("photo-%d", n)
			if n == p.dropAt {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
				} else {
					conn.Close()
				}
				return
			}
			fmt.Fprint(w, `{"ok":true,"result":true}`)
			return
		default:
			t.Error("unexpected picture method", method)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(server.Close)
	var err error
	p.pwa, err = wa.New(server.URL, "fixture-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	clients := map[string]*tg.Client{}
	for _, id := range []string{"master", "200", "201"} {
		clients[id], err = tg.New(server.URL, id, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	p.pclients = func(_ context.Context, account, id string) (metadata.PictureTelegram, error) {
		if account != "fixture-account" || clients[id] == nil {
			return nil, delivery.ErrInvalid
		}
		return clients[id], nil
	}
	p.download = metadata.NewCDN(fixtureCDN(func(r *http.Request) (*http.Response, error) {
		p.pmu.Lock()
		defer p.pmu.Unlock()
		p.downloads++
		if r.Header.Get("X-API-Key") != "" || r.Header.Get("Authorization") != "" {
			t.Error("CDN received provider credentials")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(append([]byte(nil), p.source...))), ContentLength: int64(len(p.source)), Header: http.Header{}, Request: r}, nil
	}))
	p.pictures, err = metadata.NewPictures(p.f.store, p.pwa, p.pclients, p.download, "fixture-session", -100, metadata.MaxPictureBytes)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (p *pictureFixture) run(t *testing.T, id int64) delivery.Report {
	t.Helper()
	return p.f.run(t, id, "management", func(ctx context.Context, j delivery.Job, e *delivery.Effects) error {
		if _, err := p.service.Prepare(ctx, j); err != nil {
			return err
		}
		if err := p.service.ApplyText(ctx, j, e); err != nil {
			return err
		}
		if _, err := p.pictures.Prepare(ctx, j); err != nil {
			return err
		}
		return p.pictures.Apply(ctx, j, e)
	})
}

func TestPictureRotatingURLsUseContentHashAndCurrentNativePhotoID(t *testing.T) {
	p := newPictureFixture(t)
	p.f.route(t, "123@g.us", -200, 0)
	first := p.enqueue(t, "picture-first", "group", "123@g.us")
	if r := p.run(t, first); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(p.uploads) != 1 || p.uploads[0].Method != "setChatPhoto" || p.uploads[0].Chat != "-200" {
		t.Fatal(p.uploads)
	}
	p.url = new("https://mmg.whatsapp.net/profile?token=rotated")
	second := p.enqueue(t, "picture-same", "group", "123@g.us")
	if r := p.run(t, second); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(p.uploads) != 1 || p.downloads != 2 {
		t.Fatal("rotating URL duplicated upload", len(p.uploads), p.downloads)
	}
	var skip string
	if err := p.f.store.pool.QueryRow(t.Context(), "SELECT payload->>'Skip' FROM metadata_picture_plans WHERE job_id=$1", second).Scan(&skip); err != nil || skip != "cached" {
		t.Fatal(skip, err)
	}
	p.currentID = "manual-replacement"
	third := p.enqueue(t, "picture-manual", "group", "123@g.us")
	if r := p.run(t, third); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(p.uploads) != 2 {
		t.Fatal("manual replacement hidden by source cache", len(p.uploads))
	}
}

func TestPictureHiddenSourcePreservesExistingAvatarAndCache(t *testing.T) {
	p := newPictureFixture(t)
	p.f.route(t, "123@g.us", -200, 0)
	id := p.enqueue(t, "picture-before-hidden", "group", "123@g.us")
	if r := p.run(t, id); r.State != delivery.Done {
		t.Fatal(r)
	}
	p.url = nil
	id = p.enqueue(t, "picture-hidden", "group", "123@g.us")
	if r := p.run(t, id); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(p.uploads) != 1 || p.currentID != "photo-1" || p.downloads != 1 {
		t.Fatal("hidden picture removed/replaced", p.currentID, len(p.uploads), p.downloads)
	}
	var count int
	if err := p.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM metadata_picture_cache").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestPictureContactUsesNativeStaticBotPhotoWithoutDirectRoute(t *testing.T) {
	p := newPictureFixture(t)
	p.f.bind(t, "200", "12345000000001@lid")
	id := p.enqueue(t, "picture-contact", "contact", "12345000000001@lid")
	if r := p.run(t, id); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(p.uploads) != 1 || p.uploads[0].Method != "setMyProfilePhoto" || p.uploads[0].Bot != "200" {
		t.Fatal(p.uploads)
	}
	var routes int
	if err := p.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM bridge_routes").Scan(&routes); err != nil || routes != 0 {
		t.Fatal("photo created direct route", routes, err)
	}
}

func TestPictureUnavailableDestinationsNeverFetchOrUploadAvatars(t *testing.T) {
	for _, kind := range []string{"inbox", "unbound", "permissions"} {
		t.Run(kind, func(t *testing.T) {
			p := newPictureFixture(t)
			scope, id := "group", "123@g.us"
			switch kind {
			case "inbox":
				p.f.route(t, id, -100, 44)
			case "unbound":
				scope, id = "contact", "12345000000001@lid"
				p.f.route(t, id, -100, 44)
			case "permissions":
				p.f.route(t, id, -200, 0)
				p.forbid = true
			}
			job := p.enqueue(t, "picture-skip", scope, id)
			if r := p.run(t, job); r.State != delivery.Done {
				t.Fatal(r)
			}
			if p.urlReads != 0 || p.downloads != 0 || len(p.uploads) != 0 {
				t.Fatal("ineligible destination read/uploaded avatar", p.urlReads, p.downloads, p.uploads)
			}
		})
	}
}

func TestPictureRetryReusesPersistedJPEGAfterSourceAndConsumerChange(t *testing.T) {
	p := newPictureFixture(t)
	p.f.route(t, "123@g.us", -200, 0)
	p.failAt = 1
	id := p.enqueue(t, "picture-retry", "group", "123@g.us")
	if r := p.run(t, id); r.State != delivery.Pending {
		t.Fatal(r)
	}
	original := append([]byte(nil), p.uploads[0].Data...)
	p.source = picturePNG(t, color.NRGBA{B: 255, A: 255})
	p.url = new("https://a.fbcdn.net/different")
	reopened, err := Open(t.Context(), p.f.store.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	p.pictures, err = metadata.NewPictures(reopened, p.pwa, p.pclients, p.download, "fixture-session", -100, metadata.MaxPictureBytes)
	if err != nil {
		t.Fatal(err)
	}
	p.retry(t, id)
	if r := p.run(t, id); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(p.uploads) != 2 || !bytes.Equal(p.uploads[1].Data, original) || p.urlReads != 1 || p.downloads != 1 {
		t.Fatal("retry rebuilt source or JPEG", len(p.uploads), p.urlReads, p.downloads)
	}
}

func TestPictureLostUploadResponseStaysUncertainWithoutCacheEvidence(t *testing.T) {
	p := newPictureFixture(t)
	p.f.route(t, "123@g.us", -200, 0)
	p.dropAt = 1
	id := p.enqueue(t, "picture-uncertain", "group", "123@g.us")
	if r := p.run(t, id); r.State != delivery.Uncertain {
		t.Fatal(r)
	}
	p.pmu.Lock()
	defer p.pmu.Unlock()
	var state string
	var count int
	if err := p.f.store.pool.QueryRow(t.Context(), "SELECT state FROM effects WHERE effect_key='picture-uncertain:metadata-picture'").Scan(&state); err != nil || state != "uncertain" {
		t.Fatal(state, err)
	}
	if err := p.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM metadata_picture_cache").Scan(&count); err != nil || count != 0 || len(p.uploads) != 1 {
		t.Fatal("uncertain upload became cache evidence", count, err)
	}
}

func TestPictureCacheFailureReconcilesConfirmedUploadWithoutResending(t *testing.T) {
	p := newPictureFixture(t)
	p.f.route(t, "123@g.us", -200, 0)
	if _, err := p.f.store.pool.Exec(t.Context(), `CREATE FUNCTION reject_picture_cache() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture cache failure'; END $$; CREATE TRIGGER reject_cache BEFORE INSERT ON metadata_picture_cache FOR EACH ROW EXECUTE FUNCTION reject_picture_cache()`); err != nil {
		t.Fatal(err)
	}
	id := p.enqueue(t, "picture-cache-failure", "group", "123@g.us")
	if r := p.run(t, id); r.State != delivery.Pending {
		t.Fatal(r)
	}
	p.currentID = "manual-after-observation"
	if _, err := p.f.store.pool.Exec(t.Context(), "DROP TRIGGER reject_cache ON metadata_picture_cache"); err != nil {
		t.Fatal(err)
	}
	p.retry(t, id)
	if r := p.run(t, id); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(p.uploads) != 1 || p.downloads != 1 {
		t.Fatal("confirmed picture resent", len(p.uploads), p.downloads)
	}
	var cachedID string
	if err := p.f.store.pool.QueryRow(t.Context(), "SELECT photo_id FROM metadata_picture_cache").Scan(&cachedID); err != nil || cachedID != "photo-1" {
		t.Fatal("recovery associated old source with a later manual photo", cachedID, err)
	}
	next := p.enqueue(t, "picture-after-manual-change", "group", "123@g.us")
	if r := p.run(t, next); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(p.uploads) != 2 {
		t.Fatal("manual replacement incorrectly matched recovered cache", len(p.uploads))
	}
}

func TestPicturePlanFailureRollsBackFrozenAssetBeforeUpload(t *testing.T) {
	p := newPictureFixture(t)
	p.f.route(t, "123@g.us", -200, 0)
	if _, err := p.f.store.pool.Exec(t.Context(), `CREATE FUNCTION reject_picture_plan() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture plan failure'; END $$; CREATE TRIGGER reject_picture BEFORE INSERT ON metadata_picture_plans FOR EACH ROW EXECUTE FUNCTION reject_picture_plan()`); err != nil {
		t.Fatal(err)
	}
	id := p.enqueue(t, "picture-rollback", "group", "123@g.us")
	if r := p.run(t, id); r.State != delivery.Pending {
		t.Fatal(r)
	}
	for _, table := range []string{"metadata_picture_assets", "metadata_picture_plans", "metadata_picture_cache"} {
		var count int
		if err := p.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatal(table, count, err)
		}
	}
	if len(p.uploads) != 0 {
		t.Fatal("upload preceded plan commit", p.uploads)
	}
}

func TestPictureMissingPostUploadIDDoesNotInventCacheOrResend(t *testing.T) {
	p := newPictureFixture(t)
	p.f.route(t, "123@g.us", -200, 0)
	p.missingID = true
	id := p.enqueue(t, "picture-no-id", "group", "123@g.us")
	if r := p.run(t, id); r.State != delivery.Pending {
		t.Fatal(r)
	}
	p.missingID = false
	p.retry(t, id)
	if r := p.run(t, id); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(p.uploads) != 1 {
		t.Fatal("missing ID caused duplicate upload", p.uploads)
	}
}

func TestPicturePreparedScopeRejectsRouteGenerationBindingAndPrivacyChanges(t *testing.T) {
	for _, change := range []string{"route", "generation", "binding", "public"} {
		t.Run(change, func(t *testing.T) {
			p := newPictureFixture(t)
			p.failAt = 1
			scope, id := "group", "123@g.us"
			if change == "binding" {
				scope, id = "contact", "12345000000001@lid"
				p.f.bind(t, "200", id)
			} else {
				p.f.route(t, id, -200, 0)
			}
			job := p.enqueue(t, "picture-scope", scope, id)
			if r := p.run(t, job); r.State != delivery.Pending {
				t.Fatal(r)
			}
			switch change {
			case "route":
				_, err := p.f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET telegram_chat_id=-201")
				if err != nil {
					t.Fatal(err)
				}
			case "generation":
				_, err := p.f.store.pool.Exec(t.Context(), "INSERT INTO mirror_returns(account_id,whatsapp_chat,return_key) VALUES('fixture-account','123@g.us','new-generation')")
				if err != nil {
					t.Fatal(err)
				}
			case "binding":
				p.f.bind(t, "201", id)
			case "public":
				p.public = true
			}
			p.retry(t, job)
			if r := p.run(t, job); r.State != delivery.Failed {
				t.Fatal(r)
			}
			if len(p.uploads) != 1 {
				t.Fatal("old picture retargeted/resubmitted", p.uploads)
			}
		})
	}
}

func TestPictureSourceFailurePreservesExistingCache(t *testing.T) {
	p := newPictureFixture(t)
	p.f.route(t, "123@g.us", -200, 0)
	id := p.enqueue(t, "picture-before-failure", "group", "123@g.us")
	if r := p.run(t, id); r.State != delivery.Done {
		t.Fatal(r)
	}
	p.lookupFailure = true
	id = p.enqueue(t, "picture-source-failure", "group", "123@g.us")
	if r := p.run(t, id); r.State != delivery.Pending {
		t.Fatal(r)
	}
	if len(p.uploads) != 1 || p.currentID != "photo-1" {
		t.Fatal("failed source replaced avatar", p.uploads, p.currentID)
	}
}

func TestPictureByteBudgetRejectsBeforeAnyUpload(t *testing.T) {
	p := newPictureFixture(t)
	p.f.route(t, "123@g.us", -200, 0)
	var err error
	p.pictures, err = metadata.NewPictures(p.f.store, p.pwa, p.pclients, p.download, "fixture-session", -100, 1)
	if err != nil {
		t.Fatal(err)
	}
	id := p.enqueue(t, "picture-budget", "group", "123@g.us")
	if r := p.run(t, id); r.State != delivery.Failed {
		t.Fatal(r)
	}
	if len(p.uploads) != 0 || p.currentID != "old-photo" {
		t.Fatal("oversize picture replaced avatar", p.uploads, p.currentID)
	}
	var assets int
	if err := p.f.store.pool.QueryRow(t.Context(), "SELECT count(*) FROM metadata_picture_assets").Scan(&assets); err != nil || assets != 0 {
		t.Fatal(assets, err)
	}
}

func TestPictureCacheDoesNotCrossMirrorDestinations(t *testing.T) {
	p := newPictureFixture(t)
	p.f.route(t, "123@g.us", -200, 0)
	first := p.enqueue(t, "picture-destination-one", "group", "123@g.us")
	if r := p.run(t, first); r.State != delivery.Done {
		t.Fatal(r)
	}
	if _, err := p.f.store.pool.Exec(t.Context(), "UPDATE bridge_routes SET telegram_chat_id=-201"); err != nil {
		t.Fatal(err)
	}
	second := p.enqueue(t, "picture-destination-two", "group", "123@g.us")
	if r := p.run(t, second); r.State != delivery.Done {
		t.Fatal(r)
	}
	if len(p.uploads) != 2 || p.uploads[1].Chat != "-201" {
		t.Fatal("former destination cache suppressed new destination", p.uploads)
	}
}

func TestPictureSupersedingSnapshotCannotBeOverwrittenByOldRetry(t *testing.T) {
	p := newPictureFixture(t)
	p.f.route(t, "123@g.us", -200, 0)
	p.failAt = 1
	old := p.enqueue(t, "picture-old-snapshot", "group", "123@g.us")
	if r := p.run(t, old); r.State != delivery.Pending {
		t.Fatal(r)
	}
	p.source = picturePNG(t, color.NRGBA{B: 255, A: 255})
	latest := p.enqueue(t, "picture-latest-snapshot", "group", "123@g.us")
	if r := p.run(t, latest); r.State != delivery.Done {
		t.Fatal(r)
	}
	p.retry(t, old)
	if r := p.run(t, old); r.State != delivery.Failed {
		t.Fatal(r)
	}
	if len(p.uploads) != 2 {
		t.Fatal("obsolete snapshot uploaded again", p.uploads)
	}
	var source string
	if err := p.f.store.pool.QueryRow(t.Context(), "SELECT source_hash FROM metadata_picture_cache").Scan(&source); err != nil || source != metadata.PictureHash(p.source) {
		t.Fatal(source, err)
	}
}

func TestPictureClaimFencesAssetReadsAndCacheWrites(t *testing.T) {
	p := newPictureFixture(t)
	p.f.route(t, "123@g.us", -200, 0)
	p.failAt = 1
	id := p.enqueue(t, "picture-fence", "group", "123@g.us")
	if r := p.run(t, id); r.State != delivery.Pending {
		t.Fatal(r)
	}
	p.retry(t, id)
	j := claim(t, p.f.store)
	foreign := j
	foreign.AccountID = "another-account"
	if _, err := p.f.store.MetadataPictureAsset(t.Context(), foreign); !errors.Is(err, delivery.ErrClaimLost) {
		t.Fatal(err)
	}
	if err := p.f.store.CompleteMetadataPicture(t.Context(), foreign, "invented"); !errors.Is(err, delivery.ErrClaimLost) {
		t.Fatal(err)
	}
	if _, err := p.f.store.pool.Exec(t.Context(), "UPDATE jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.f.store.MetadataPictureAsset(t.Context(), j); !errors.Is(err, delivery.ErrClaimLost) {
		t.Fatal(err)
	}
	if err := p.f.store.CompleteMetadataPicture(t.Context(), j, "invented"); !errors.Is(err, delivery.ErrClaimLost) {
		t.Fatal(err)
	}
}

func TestPictureCorruptFrozenAssetCannotReachProvider(t *testing.T) {
	p := newPictureFixture(t)
	p.f.route(t, "123@g.us", -200, 0)
	p.failAt = 1
	id := p.enqueue(t, "picture-corrupt", "group", "123@g.us")
	if r := p.run(t, id); r.State != delivery.Pending {
		t.Fatal(r)
	}
	if _, err := p.f.store.pool.Exec(t.Context(), "UPDATE metadata_picture_assets SET jpeg=$1", []byte("corrupt")); err != nil {
		t.Fatal(err)
	}
	p.retry(t, id)
	if r := p.run(t, id); r.State != delivery.Failed {
		t.Fatal(r)
	}
	if len(p.uploads) != 1 {
		t.Fatal("corrupt asset uploaded", p.uploads)
	}
}
