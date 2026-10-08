package implement

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
)

// This file exercises the picture use cases over the in-memory repository and a
// fake media service. It proves the two halves of FR-016 — the first picture is
// the main one and removing it promotes the next — that the ceiling is enforced
// before the provider is called, and that the count check and the insert share
// the caller's transaction while the provider call does not (research D6).

// fauxUnitOfWork is an in-memory UnitOfWork. It marks the context so the
// repository and the media fake can record whether a call ran inside the
// transaction.
type fauxUnitOfWork struct {
	mu     sync.Mutex
	calls  int
	active int
}

func (u *fauxUnitOfWork) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	u.mu.Lock()
	u.calls++
	u.active++
	u.mu.Unlock()
	err := fn(context.WithValue(ctx, txKey{}, true))
	u.mu.Lock()
	u.active--
	u.mu.Unlock()
	return err
}

// fakeMedia answers the MediaStore port without a provider. It records whether
// each upload ran inside a transaction, which is what proves the provider call
// stays outside it.
type fakeMedia struct {
	mu         sync.Mutex
	uploads    int
	removed    int
	uploadInTx []bool
	reference  appinterface.MediaReference
	err        error
}

func newFakeMedia() *fakeMedia {
	return &fakeMedia{reference: appinterface.MediaReference{
		PublicID: "artist-shop/products/one",
		URL:      "https://cdn.example.test/products/one.jpg",
		Width:    1600,
		Height:   1200,
	}}
}

func (m *fakeMedia) Upload(ctx context.Context, _ []byte, _ int) (appinterface.MediaReference, error) {
	m.mu.Lock()
	m.uploads++
	m.uploadInTx = append(m.uploadInTx, inTx(ctx))
	reference, err := m.reference, m.err
	m.mu.Unlock()
	if err != nil {
		return appinterface.MediaReference{}, err
	}
	return reference, nil
}

func (m *fakeMedia) Remove(context.Context, appinterface.MediaReference) error {
	m.mu.Lock()
	m.removed++
	err := m.err
	m.mu.Unlock()
	return err
}

var _ appinterface.MediaStore = (*fakeMedia)(nil)

// pngBytes is a payload the domain sniffer recognises as a PNG.
func pngBytes() []byte {
	return append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x2A}, 16)...)
}

// pictureFixture builds the service with a recording auditor and a fake media
// service, and returns all three so a test can assert the effects.
type pictureFixture struct {
	service *Service
	repo    *fauxProducts
	media   *fakeMedia
	audit   *recordingAuditor
	actor   appinterface.Actor
	tx      *fauxUnitOfWork
}

func newPictureFixture(t *testing.T) *pictureFixture {
	t.Helper()
	repo := newFauxProducts()
	media := newFakeMedia()
	audit := &recordingAuditor{}
	tx := &fauxUnitOfWork{}
	actor := adminActor()
	service := New(Service{
		Products: repo,
		Media:    media,
		Tx:       tx,
		Audit:    audit,
		Mapper:   mapper.New(),
	})
	return &pictureFixture{service: service, repo: repo, media: media, audit: audit, actor: actor, tx: tx}
}

// seedProduct stores one product and returns it.
func (f *pictureFixture) seedProduct(t *testing.T, slug string) *model.Product {
	t.Helper()
	return f.repo.seed(t, model.ProductDraft{
		Name: "Product " + slug, Slug: slug, Price: price(100), CategoryID: uuid.New(),
	}, time.Now().UTC().Add(-time.Hour))
}

func (f *pictureFixture) ctx() context.Context {
	return appinterface.WithActor(context.Background(), f.actor)
}

// FR-016: the first picture added becomes the main one, and the asset is stored
// through the media port.
func TestTheFirstPictureAddedBecomesTheMainOne(t *testing.T) {
	f := newPictureFixture(t)
	product := f.seedProduct(t, "first-picture")

	out, err := f.service.AddPicture(f.ctx(), dto.AddPictureInput{ProductID: product.ID, Content: pngBytes()})
	if err != nil {
		t.Fatalf("AddPicture: %v", err)
	}
	if len(out.Images) != 1 {
		t.Fatalf("expected one picture, got %d", len(out.Images))
	}
	if !out.Images[0].IsPrimary {
		t.Fatalf("the first picture added must be the main one: %+v", out.Images[0])
	}
	if out.Images[0].URL != "https://cdn.example.test/products/one.jpg" {
		t.Fatalf("only the provider's reference must be stored, got %+v", out.Images[0])
	}
	if f.media.uploads != 1 {
		t.Fatalf("expected one upload, got %d", f.media.uploads)
	}
	requireAudited(t, f.audit, constant.AuditProductImageAdded, f.actor, product.ID)
}

// FR-016, research D7: removing the main picture promotes the next by position
// and releases the stored asset.
func TestRemovingTheMainOnePromotesTheNextByPosition(t *testing.T) {
	f := newPictureFixture(t)
	product := f.seedProduct(t, "promote-picture")
	first := f.repo.addSeedPicture(product.ID, 0, true)
	second := f.repo.addSeedPicture(product.ID, 1, false)
	f.repo.addSeedPicture(product.ID, 2, false)

	if err := f.service.RemovePicture(f.ctx(), dto.RemovePictureInput{
		ProductID: product.ID, PictureID: first.ID,
	}); err != nil {
		t.Fatalf("RemovePicture: %v", err)
	}

	pictures, err := f.repo.ListPictures(context.Background(), product.ID)
	if err != nil {
		t.Fatalf("ListPictures: %v", err)
	}
	if len(pictures) != 2 {
		t.Fatalf("expected two pictures left, got %d", len(pictures))
	}
	primary, ok := model.PrimaryPicture(pictures)
	if !ok || primary.ID != second.ID {
		t.Fatalf("removing the main picture must promote the next by position, got %+v", pictures)
	}
	if f.media.removed != 1 {
		t.Fatalf("expected the stored asset to be released once, got %d", f.media.removed)
	}
	requireAudited(t, f.audit, constant.AuditProductImageRemoved, f.actor, product.ID)
}

// FR-020, quickstart 10g: the eleventh picture is refused before the media store
// is called, so nothing is uploaded and nothing is audited.
func TestTheEleventhIsRefusedBeforeTheMediaStoreIsCalled(t *testing.T) {
	f := newPictureFixture(t)
	product := f.seedProduct(t, "ceiling-picture")
	for i := 0; i < model.MaxProductPictures; i++ {
		f.repo.addSeedPicture(product.ID, i, i == 0)
	}

	_, err := f.service.AddPicture(f.ctx(), dto.AddPictureInput{ProductID: product.ID, Content: pngBytes()})
	if !errors.Is(err, domainerr.ErrProductImageLimitReached) {
		t.Fatalf("expected ErrProductImageLimitReached, got %v", err)
	}
	if f.media.uploads != 0 {
		t.Fatalf("a refused eleventh picture must not reach the media store, got %d uploads", f.media.uploads)
	}
	for _, action := range f.audit.actions() {
		if action == constant.AuditProductImageAdded {
			t.Fatalf("a refused upload must not be audited: %v", f.audit.actions())
		}
	}
}

// FR-016: a picture that does not belong to the product answers not-found, both
// when removed and when promoted.
func TestAPictureThatDoesNotBelongToTheProductAnswersNotFound(t *testing.T) {
	f := newPictureFixture(t)
	owner := f.seedProduct(t, "owner-product")
	other := f.seedProduct(t, "other-product")
	picture := f.repo.addSeedPicture(owner.ID, 0, true)

	if err := f.service.RemovePicture(f.ctx(), dto.RemovePictureInput{
		ProductID: other.ID, PictureID: picture.ID,
	}); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("removing a foreign picture must answer not-found, got %v", err)
	}
	if _, err := f.service.SetPrimaryPicture(f.ctx(), dto.SetPrimaryPictureInput{
		ProductID: other.ID, PictureID: picture.ID,
	}); !errors.Is(err, domainerr.ErrProductNotFound) {
		t.Fatalf("promoting a foreign picture must answer not-found, got %v", err)
	}
	if f.media.removed != 0 {
		t.Fatalf("a refused removal must not release anything, got %d", f.media.removed)
	}
}

// research D6: the ceiling check and the insert run inside the UnitOfWork
// transaction, and the provider call stays outside it.
func TestTheCountCheckAndTheInsertShareTheTransactionAndTheProviderIsOutside(t *testing.T) {
	f := newPictureFixture(t)
	product := f.seedProduct(t, "transaction-picture")

	if _, err := f.service.AddPicture(f.ctx(), dto.AddPictureInput{ProductID: product.ID, Content: pngBytes()}); err != nil {
		t.Fatalf("AddPicture: %v", err)
	}

	if f.tx.calls == 0 {
		t.Fatal("the picture use case must run inside the UnitOfWork transaction")
	}
	for _, call := range append(f.repo.ops("count"), f.repo.ops("insert")...) {
		if !call.inTx {
			t.Fatalf("the %s must run inside the transaction (research D6)", call.op)
		}
	}
	if len(f.media.uploadInTx) != 1 || f.media.uploadInTx[0] {
		t.Fatalf("the provider call must stay outside the transaction, got %v", f.media.uploadInTx)
	}
}
