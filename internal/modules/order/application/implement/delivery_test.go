package implement

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/media"
)

// This file is the use-case contract of US1: dispatching a paid order with
// evidence. StartShippingByAdmin drives PAID -> SHIPPING, uploads zero to five
// photos and persists their references; it refuses a non-PAID order naming the
// current state and an over-ceiling or unsupported/oversized photo BEFORE the
// media store is called, so nothing is uploaded on a request it will refuse
// (FR-005, FR-006, FR-009). It runs the real use case over in-memory fakes of the
// repository, the auditor and the media store, so the answers asserted are the
// service's own.

// deliveryOrders is the in-memory OrderRepository the delivery use cases use. Its
// row lock returns a copy — the way the postgres adapter scans a fresh row — so
// the domain transition the use case drives during its pre-upload check never
// mutates what the repository holds before the write commits. It records the
// dispatch writes and the photo rows so a test can assert them.
type deliveryOrders struct {
	appinterface.OrderRepository
	byID    map[uuid.UUID]*model.Order
	photos  map[uuid.UUID][]model.DeliveryPhoto
	shipped []uuid.UUID
}

func (r *deliveryOrders) LockByID(_ context.Context, id uuid.UUID) (*model.Order, error) {
	order, ok := r.byID[id]
	if !ok {
		return nil, domainerr.ErrNotFound
	}
	clone := *order
	return &clone, nil
}

func (r *deliveryOrders) StartShipping(_ context.Context, order *model.Order, _ time.Time) error {
	if stored, ok := r.byID[order.ID]; ok {
		stored.Status = order.Status
	}
	r.shipped = append(r.shipped, order.ID)
	return nil
}

func (r *deliveryOrders) CountDeliveryPhotos(_ context.Context, orderID uuid.UUID, stage model.Stage) (int, error) {
	count := 0
	for _, photo := range r.photos[orderID] {
		if photo.Stage == stage {
			count++
		}
	}
	return count, nil
}

func (r *deliveryOrders) AddDeliveryPhotos(_ context.Context, orderID uuid.UUID, photos []model.DeliveryPhoto, _ time.Time) error {
	r.photos[orderID] = append(r.photos[orderID], photos...)
	return nil
}

// fakeMediaStore is the in-memory MediaStore. It records every uploaded payload
// and every released reference, so a test can prove validation happens before any
// upload and that a failed write releases what it stored. err makes Upload fail.
type fakeMediaStore struct {
	uploaded [][]byte
	removed  []media.Reference
	err      error
}

func (m *fakeMediaStore) Upload(_ context.Context, content []byte, _ int) (media.Reference, error) {
	if m.err != nil {
		return media.Reference{}, m.err
	}
	m.uploaded = append(m.uploaded, content)
	n := len(m.uploaded)
	return media.Reference{
		PublicID: fmt.Sprintf("public-%d", n),
		URL:      fmt.Sprintf("https://cdn.example/%d.jpg", n),
		Width:    100,
		Height:   80,
	}, nil
}

func (m *fakeMediaStore) Remove(_ context.Context, ref media.Reference) error {
	m.removed = append(m.removed, ref)
	return nil
}

// deliveryFixture is the real delivery use case over in-memory fakes.
type deliveryFixture struct {
	svc    *Service
	orders *deliveryOrders
	media  *fakeMediaStore
	audit  *recordingAuditor
}

func newDeliveryFixture(order *model.Order) *deliveryFixture {
	store := &deliveryOrders{
		byID:   map[uuid.UUID]*model.Order{order.ID: order},
		photos: map[uuid.UUID][]model.DeliveryPhoto{},
	}
	recorder := &recordingAuditor{}
	mediaStore := &fakeMediaStore{}
	svc := New(Service{
		Orders: store,
		Tx:     passthroughTx{},
		Clock:  memoryClock{},
		Mapper: mapper.New(),
		Audit:  recorder,
		Media:  mediaStore,
	})
	return &deliveryFixture{svc: svc, orders: store, media: mediaStore, audit: recorder}
}

// jpeg is a minimal payload whose leading bytes are a JPEG signature, so it
// passes the format sniff. The trailing bytes stand in for image content.
func jpeg() []byte { return []byte{0xFF, 0xD8, 0xFF, 0x00, 0x11, 0x22} }

// dispatchableOrder builds a PAID order ready to dispatch.
func dispatchableOrder() *model.Order {
	order := orderAt(uuid.New(), fixedNow, line(uuid.New(), 1000, 1))
	order.Status = constant.StatusPaid
	return order
}

// FR-005, FR-006, FR-009: dispatching a paid order with photos moves it SHIPPING,
// uploads each photo through the media store, persists their references at the
// SHIPPING stage and records the act naming the order and the administrator.
func TestStartShippingByAdminDrivesPaidToShippingWithPhotos(t *testing.T) {
	admin := uuid.New()
	order := dispatchableOrder()
	f := newDeliveryFixture(order)

	view, err := f.svc.StartShippingByAdmin(adminContext(admin), appdto.EvidenceInput{
		OrderID: order.ID,
		Photos:  []appdto.UploadedPhoto{{Content: jpeg()}, {Content: jpeg()}},
	})
	if err != nil {
		t.Fatalf("StartShippingByAdmin: %v", err)
	}
	if view.Status != constant.StatusShipping {
		t.Fatalf("the order must answer SHIPPING, got %s", view.Status)
	}
	if order.Status != constant.StatusShipping {
		t.Fatalf("the persisted state must be SHIPPING, got %s", order.Status)
	}

	if len(f.media.uploaded) != 2 {
		t.Fatalf("every photo must reach the media store, got %d uploads", len(f.media.uploaded))
	}
	stored := f.orders.photos[order.ID]
	if len(stored) != 2 {
		t.Fatalf("both photos must be persisted, got %d", len(stored))
	}
	for i, photo := range stored {
		if photo.Stage != model.StageShipping {
			t.Fatalf("photo %d must be SHIPPING-stage evidence, got %s", i, photo.Stage)
		}
		if photo.Position != i {
			t.Fatalf("photo %d must be positioned %d, got %d", i, i, photo.Position)
		}
		if photo.PublicID == "" || photo.URL == "" {
			t.Fatalf("photo %d must carry the provider reference, got %+v", i, photo)
		}
	}

	if len(f.audit.events) != 1 {
		t.Fatalf("dispatching must record one audit entry, got %+v", f.audit.events)
	}
	assertAudit(t, f.audit.events[0], constant.AuditOrderShipped, admin, order.ID)
}

// FR-006: photos are optional; a dispatch with no photo still moves the order and
// never calls the media store.
func TestStartShippingByAdminAcceptsNoPhotos(t *testing.T) {
	order := dispatchableOrder()
	f := newDeliveryFixture(order)

	view, err := f.svc.StartShippingByAdmin(adminContext(uuid.New()), appdto.EvidenceInput{OrderID: order.ID})
	if err != nil {
		t.Fatalf("StartShippingByAdmin: %v", err)
	}
	if view.Status != constant.StatusShipping || order.Status != constant.StatusShipping {
		t.Fatalf("a dispatch with no photos must still move the order, got %s/%s", view.Status, order.Status)
	}
	if len(f.media.uploaded) != 0 {
		t.Fatalf("a dispatch with no photos must not call the media store, got %d uploads", len(f.media.uploaded))
	}
	if len(f.orders.photos[order.ID]) != 0 {
		t.Fatalf("a dispatch with no photos must persist no evidence, got %+v", f.orders.photos[order.ID])
	}
	if len(f.audit.events) != 1 {
		t.Fatalf("the dispatch must still be audited, got %+v", f.audit.events)
	}
}

// FR-005: dispatching an order that is not PAID is refused naming the current
// state, and neither the order nor the media store is touched.
func TestStartShippingByAdminRefusesANondispatchableOrderBeforeUpload(t *testing.T) {
	for _, status := range []constant.Status{
		constant.StatusPending,
		constant.StatusPaymentPending,
		constant.StatusShipping,
		constant.StatusDelivered,
		constant.StatusCompleted,
		constant.StatusCancelled,
	} {
		order := orderAt(uuid.New(), fixedNow, line(uuid.New(), 1000, 1))
		order.Status = status
		f := newDeliveryFixture(order)

		_, err := f.svc.StartShippingByAdmin(adminContext(uuid.New()), appdto.EvidenceInput{
			OrderID: order.ID,
			Photos:  []appdto.UploadedPhoto{{Content: jpeg()}},
		})
		var refusal *domainerr.StateTransitionError
		if !errors.As(err, &refusal) {
			t.Fatalf("status %s: expected a state transition refusal, got %v", status, err)
		}
		if refusal.From != status || refusal.To != constant.StatusShipping {
			t.Fatalf("status %s: the refusal must name the current state, got %+v", status, refusal)
		}
		if order.Status != status {
			t.Fatalf("status %s: a refused dispatch must leave the order untouched, got %s", status, order.Status)
		}
		if len(f.media.uploaded) != 0 {
			t.Fatalf("status %s: a refused dispatch must not reach the media store", status)
		}
		if len(f.audit.events) != 0 {
			t.Fatalf("status %s: a refused dispatch must audit nothing, got %+v", status, f.audit.events)
		}
	}
}

// FR-006: more than five photos are refused before the media store is called and
// leave the order exactly as it was.
func TestStartShippingByAdminRefusesMoreThanFivePhotosBeforeUpload(t *testing.T) {
	order := dispatchableOrder()
	f := newDeliveryFixture(order)

	photos := make([]appdto.UploadedPhoto, 6)
	for i := range photos {
		photos[i] = appdto.UploadedPhoto{Content: jpeg()}
	}
	_, err := f.svc.StartShippingByAdmin(adminContext(uuid.New()), appdto.EvidenceInput{OrderID: order.ID, Photos: photos})
	if !errors.Is(err, domainerr.ErrImageLimitReached) {
		t.Fatalf("six photos must be refused with ORDER_IMAGE_LIMIT_REACHED, got %v", err)
	}
	if len(f.media.uploaded) != 0 {
		t.Fatalf("an over-ceiling request must not reach the media store, got %d uploads", len(f.media.uploaded))
	}
	if order.Status != constant.StatusPaid {
		t.Fatalf("an over-ceiling request must leave the order PAID, got %s", order.Status)
	}
}

// FR-009: an unsupported or oversized photo is refused before anything is
// uploaded, naming the refusal a client maps to ORDER_IMAGE_TYPE_UNSUPPORTED or
// ORDER_IMAGE_TOO_LARGE. The size ceiling is the domain constant
// model.MaxDeliveryPhotoBytes — the single source the handler and the use case
// share — so the oversized payload is built one byte over it.
func TestStartShippingByAdminRefusesABadPhotoBeforeUpload(t *testing.T) {
	cases := []struct {
		name    string
		content []byte
		want    error
	}{
		{"unsupported format", []byte("this is not an image"), domainerr.ErrImageTypeUnsupported},
		{"over the ceiling", append(jpeg(), bytes.Repeat([]byte{0x00}, int(model.MaxDeliveryPhotoBytes))...), domainerr.ErrImageTooLarge},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			order := dispatchableOrder()
			f := newDeliveryFixture(order)

			_, err := f.svc.StartShippingByAdmin(adminContext(uuid.New()), appdto.EvidenceInput{
				OrderID: order.ID,
				Photos:  []appdto.UploadedPhoto{{Content: tc.content}},
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
			if len(f.media.uploaded) != 0 {
				t.Fatalf("a refused photo must not reach the media store, got %d uploads", len(f.media.uploaded))
			}
			if order.Status != constant.StatusPaid {
				t.Fatalf("a refused photo must leave the order PAID, got %s", order.Status)
			}
		})
	}
}
