package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	productimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/implement"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
)

// The product module derives one folding key, NormalizedSlug, from the slug so
// the storage layer can enforce catalogue-wide uniqueness (FR-028). It exists
// only to be stored, indexed and compared: it is never returned to a caller,
// never written to an audit entry and never named in the contract. Unlike a
// category's folded name, this key's *value* is indistinguishable from the
// stored slug — a valid slug is already lowercase and trimmed, so
// FoldKey(slug) == slug — and the slug legitimately appears in responses and
// audit entries. The assertion therefore pins the folding-key *member* (the
// name it could only be carried under), which is the only thing a valid
// catalogue can distinguish.

// productLeakPublicPath is the public group's mount point, repeated here so the
// leak test can reuse the fixtures the browse tests own.
const productLeakPublicPath = "/api/v1/products"

// foldingKeyMembers are the spellings a folding key could reach a body under:
// the Go member, the JSON member and the storage column. Matching is
// case-insensitive, so "normalizedslug" covers all three spellings.
const foldedSlugMember = "normalizedslug"

// leakAuditor captures the metadata map the use case hands the audit port, so a
// test can assert what an audit entry would actually carry.
type leakAuditor struct {
	mu       sync.Mutex
	metadata []map[string]any
}

func (a *leakAuditor) Record(_ context.Context, _, _ string, _ *uuid.UUID, _, _, _ string, metadata map[string]any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.metadata = append(a.metadata, metadata)
}

var _ appinterface.Auditor = (*leakAuditor)(nil)

func (a *leakAuditor) entries() []map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]map[string]any, len(a.metadata))
	copy(out, a.metadata)
	return out
}

// assertNoFoldingKeyMember asserts a rendered body, serialized metadata or log
// line carries no member a folding key could be exposed under.
func assertNoFoldingKeyMember(t *testing.T, what, got string) {
	t.Helper()
	if strings.Contains(strings.ToLower(got), foldedSlugMember) {
		t.Fatalf("%s carries the folding-key member %q: %s", what, foldedSlugMember, got)
	}
}

// leakRepo is the in-memory repository the leak fixture needs. It adds the one
// administrator read the responses test exercises (the list) on top of the
// upload fixture's writes, so every administrator shape is reachable.
type leakRepo struct {
	*uploadRepo
}

func (r *leakRepo) ListAll(_ context.Context, _, _ int) ([]domainrepo.ProductListItem, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]domainrepo.ProductListItem, 0, len(r.products))
	for _, product := range r.products {
		items = append(items, domainrepo.ProductListItem{
			Product:      product,
			PictureCount: len(r.pictures[product.ID]),
		})
	}
	return items, int64(len(items)), nil
}

// leakFixture mounts the real product use cases for both audiences over an
// in-memory repository, the media stub and a capturing auditor, with the
// handler's logger writing to a buffer so the log line can be asserted too.
type leakFixture struct {
	router http.Handler
	repo   *leakRepo
	media  *productMediaStub
	audit  *leakAuditor
	logs   *bytes.Buffer
}

func newLeakFixture(t *testing.T) *leakFixture {
	t.Helper()
	repo := &leakRepo{uploadRepo: newUploadRepo()}
	media := newProductMediaStub()
	audit := &leakAuditor{}
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	cfg := appinterface.Config{PictureMaxBytes: 4096}
	service := productimplement.New(productimplement.Service{
		Products: repo,
		Media:    media,
		Tx:       httpUnitOfWork{},
		Config:   cfg,
		Audit:    audit,
		Mapper:   mapper.New(),
	})
	handler := New(service, cfg, logger)
	root := chi.NewRouter()
	root.Mount(adminProductsPath, handler.AdminRouter(maintenanceHooks(nil)))
	return &leakFixture{router: root, repo: repo, media: media, audit: audit, logs: logs}
}

// FR-008, FR-013, Constitution V and VI: the folding key reaches no response
// and no audit entry. Every body is also asserted to still carry the operator's
// slug, so a response that had swallowed everything would fail instead of
// passing silently.
func TestTheFoldingKeyNeverReachesAResponseOrAnAuditEntry(t *testing.T) {
	f := newLeakFixture(t)
	const slug = "diau-khac"
	createBody := `{"name":"Điêu khắc","slug":"` + slug + `","description":"một mô tả",` +
		`"price":{"amount":120000,"currency":"VND"},"categoryId":"` + uuid.New().String() + `","position":1}`

	created := performJSON(f.router, http.MethodPost, adminProductsPath, createBody, "admin-token")
	if created.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", created.Code, created.Body.String())
	}
	id := decodeAdminProduct(t, created).ID

	rendered := map[string]string{
		"the create answer":        created.Body.String(),
		"the administrator detail": performJSON(f.router, http.MethodGet, adminProductsPath+"/"+id.String(), "", "admin-token").Body.String(),
		"the administrator list":   performJSON(f.router, http.MethodGet, adminProductsPath, "", "admin-token").Body.String(),
		"the edit answer": performJSON(f.router, http.MethodPatch, adminProductsPath+"/"+id.String(),
			`{"name":"Điêu khắc mới"}`, "admin-token").Body.String(),
	}
	for what, body := range rendered {
		assertNoFoldingKeyMember(t, what, body)
		if !strings.Contains(body, slug) {
			t.Fatalf("%s must still carry the operator's slug, got %s", what, body)
		}
	}

	// The captured audit metadata is what an audit entry would persist. It
	// carries the operator's slug and nothing derived from it: no folding key,
	// and no key a folding key could be named under.
	entries := f.audit.entries()
	if len(entries) != 2 {
		t.Fatalf("expected one audit entry from the create and one from the edit, got %d", len(entries))
	}
	for i, metadata := range entries {
		serialized, err := json.Marshal(metadata)
		if err != nil {
			t.Fatalf("serialize the captured audit metadata: %v", err)
		}
		assertNoFoldingKeyMember(t, "the audit entry", string(serialized))
		if len(metadata) != 1 || metadata["slug"] != slug {
			t.Fatalf("audit entry %d must carry only the operator's slug, got %v", i, metadata)
		}
	}
}

// FR-008: the public shapes carry the operator's slug and none of the
// administrator members, the folding key included.
func TestThePublicShapesCarryNoFoldingKey(t *testing.T) {
	handler, repo, visibility := newPublicFixture(t)
	slug := "diau-khac"
	product, _ := seedVisibleProduct(t, repo, visibility, slug, 1)
	repo.addPicture(model.Picture{
		ID: uuid.New(), ProductID: product.ID, PublicID: "p1",
		URL: "https://cdn.example.test/stand.jpg", Width: 800, Height: 600,
		Position: 0, IsPrimary: true,
	})

	list := perform(handler, http.MethodGet, productLeakPublicPath).Body.String()
	detail := perform(handler, http.MethodGet, productLeakPublicPath+"/"+slug).Body.String()
	for what, body := range map[string]string{"the public list": list, "the public detail": detail} {
		assertNoFoldingKeyMember(t, what, body)
		if !strings.Contains(body, slug) {
			t.Fatalf("%s must still carry the operator's slug, got %s", what, body)
		}
	}
}

// FR-018, Constitution V and VI: a media failure is reported without the
// provider's URL, credential or response body. The stub's error carries all
// three, so the test proves the use case drops them before the handler logs.
func TestAMediaFailureLogsNoProviderDetail(t *testing.T) {
	f := newLeakFixture(t)
	product := f.repo.seed(t, "media-failure")

	const (
		providerURL  = "https://api.cloudinary.example/v1_1/secret-cloud/image/upload"
		credential   = "api_key=1234567890"
		providerBody = `{"error":{"message":"Invalid credentials for secret-cloud"}}`
	)
	f.media.err = errors.New(providerURL + " " + credential + " " + providerBody)

	path := adminProductsPath + "/" + product.ID.String() + "/images"
	rec := postImage(f.router, path, "photo.jpg", "image/jpeg", jpegPayload(), "admin-token")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected the retryable 503, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeAdminError(t, rec); body.Error.Code != "PRODUCT_MEDIA_UNAVAILABLE" {
		t.Fatalf("expected PRODUCT_MEDIA_UNAVAILABLE, got %s", body.Error.Code)
	}

	logLine := f.logs.String()
	if !strings.Contains(logLine, "request failed") {
		t.Fatalf("expected the failure log line, got %q", logLine)
	}
	assertNoFoldingKeyMember(t, "the log line", logLine)
	for _, leak := range []string{providerURL, credential, providerBody, "secret-cloud"} {
		if strings.Contains(logLine, leak) {
			t.Fatalf("the log line must not carry provider detail %q: %s", leak, logLine)
		}
		if strings.Contains(rec.Body.String(), leak) {
			t.Fatalf("the response must not carry provider detail %q: %s", leak, rec.Body.String())
		}
	}
}
