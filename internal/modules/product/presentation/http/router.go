package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// pictureUploadOverhead is the room the upload routes leave for the multipart
// envelope — part headers, boundaries and the form field name — on top of the
// image ceiling. The image itself is capped again while it is read, so this only
// has to be generous, never exact.
const pictureUploadOverhead = 64 << 10

// PictureUploadCeiling is the body ceiling the picture upload routes install: the
// image ceiling plus the room the multipart envelope needs. The routes and the
// handler read this function, so the limit a route applies cannot drift from the
// ceiling the use case enforces.
func PictureUploadCeiling(imageCeilingBytes int64) int64 {
	return imageCeilingBytes + pictureUploadOverhead
}

// Router builds the public `/products` group. Mount it under `/api/v1`.
//
// Both routes are readable without a session (FR-001): the list is at the group
// root and the detail is addressed by code, because the code is what a
// customer-facing link is built from. The administrator surface, which is
// addressed by identifier, is the separate AdminRouter group below.
func (h *Handler) Router(_ middleware.AuthHooks) http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.ListPublic)
	r.Get("/{code}", h.GetPublicByCode)
	return r
}

// AdminRouter builds the `/admin/products` group. Mount it under `/api/v1`.
//
// Every route is guarded by the administrator role (FR-014), and the denial is
// recorded through the foundation's OnDenied hook the composition root supplies,
// exactly as module 03 audits its own privilege denial. The surface is addressed
// by identifier because the operator reaches it from the administrator list and an
// identifier cannot change under a client that is mid-edit.
func (h *Handler) AdminRouter(hooks middleware.AuthHooks) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequireRole(hooks, string(access.RoleAdmin)))
	r.Get("/", h.ListAdmin)
	r.Post("/", h.CreateProduct)
	r.Get("/{id}", h.GetAdmin)
	r.Patch("/{id}", h.UpdateProduct)
	r.Delete("/{id}", h.DeleteProduct)
	// The variants of a product are their own sub-resource: a variant is added,
	// edited, removed, pictured and moved through its own routes (FR-012, FR-015,
	// FR-016).
	r.Post("/{id}/variants", h.AddVariant)
	r.Patch("/{id}/variants/{variantId}", h.UpdateVariant)
	r.Delete("/{id}/variants/{variantId}", h.RemoveVariant)
	// The variant image upload raises the body limit for its own handler; the
	// handler caps the image itself again while it reads.
	r.With(middleware.BodyLimitWithRefusal(
		PictureUploadCeiling(h.cfg.PictureMaxBytesOrDefault()), imageTooLarge(),
	)).Post("/{id}/variants/{variantId}/image", h.AddVariantImage)
	r.Delete("/{id}/variants/{variantId}/image", h.RemoveVariantImage)
	// A variant's state change is its own endpoint: a transition is not a field
	// edit and it can be refused, which is what the surface has to express
	// (FR-015).
	r.Post("/{id}/variants/{variantId}/state", h.ChangeVariantState)
	// The product gallery keeps the picture routes unchanged.
	r.With(middleware.BodyLimitWithRefusal(
		PictureUploadCeiling(h.cfg.PictureMaxBytesOrDefault()), imageTooLarge(),
	)).Post("/{id}/images", h.AddPicture)
	r.Delete("/{id}/images/{imageId}", h.RemovePicture)
	r.Post("/{id}/images/{imageId}/primary", h.SetPrimaryPicture)
	return r
}
