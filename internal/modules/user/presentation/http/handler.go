// Package httpapi exposes the user module over HTTP. It owns the /users and
// /divisions routes and carries no business rules: handlers translate HTTP to
// application DTOs and map sentinel errors to the shared error envelope.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/constant"
	httpdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/presentation/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// Pagination bounds of the address list (contracts/openapi.yaml, Page/PageSize).
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// Handler adapts the user use cases to HTTP.
type Handler struct {
	svc    appinterface.UserService
	cfg    appinterface.Config
	logger *slog.Logger
}

// New creates the user HTTP handler.
//
// The auditor is deliberately not a dependency here: audit events for profile,
// avatar and address changes are recorded by the use cases, and a privilege
// denial is recorded by the composition root's authentication hooks through the
// middleware's OnDenied callback, exactly as the auth module does it.
func New(svc appinterface.UserService, cfg appinterface.Config, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, cfg: cfg, logger: logger}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	httpx.WriteError(w, r, mapError(err), h.logger)
}

func (h *Handler) failAddress(w http.ResponseWriter, r *http.Request, err error) {
	httpx.WriteError(w, r, mapAddressError(err), h.logger)
}

// decode reads a JSON body and rejects unknown members, matching the
// additionalProperties: false of the request schemas.
func decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// sessionAccount returns the acting account taken from the authenticated session.
//
// No route accepts an owner identifier: the session is the only source of the
// acting account, which is what makes cross-account access impossible by
// construction (FR-006, research D5).
func (h *Handler) sessionAccount(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	actor, ok := h.sessionActor(w, r)
	return actor.ID, ok
}

// sessionActor returns the acting account together with the role its session
// carries. Only the operator lookup needs the role: the audit row it writes has to
// name who looked, and a role taken from the token is a privilege the caller really
// holds rather than one it claimed.
//
// A missing or unparseable identity answers UNAUTHENTICATED, which on the
// self-service routes is a client mistake and behind the ADMIN guard is an invalid
// session this service cannot produce.
func (h *Handler) sessionActor(w http.ResponseWriter, r *http.Request) (appinterface.Actor, bool) {
	identity, ok := middleware.IdentityFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, httpx.New(httpx.CodeUnauthenticated), h.logger)
		return appinterface.Actor{}, false
	}
	accountID, err := uuid.Parse(identity.Subject)
	if err != nil {
		// The auth module only issues tokens whose subject is a UUID, so this is
		// an invalid session rather than a client mistake.
		httpx.WriteError(w, r, httpx.New(httpx.CodeUnauthenticated), h.logger)
		return appinterface.Actor{}, false
	}
	return appinterface.Actor{ID: accountID, Role: access.Role(identity.Role)}, true
}

// pathUUID reads a UUID path parameter. A value that is not a UUID is a request
// error, never a lookup that returns nothing: it answers
// 400 VALIDATION_ERROR with the offending member named (FR-020).
func pathUUID(r *http.Request, name, field string) (uuid.UUID, *httpx.AppError) {
	raw := chi.URLParam(r, name)
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fieldError(field, "must be a UUID")
	}
	return parsed, nil
}

// pageParams reads the address-list window. Pagination validation is a foundation
// concern and stays on the shared VALIDATION_ERROR code.
func pageParams(r *http.Request) (int, int, *httpx.AppError) {
	query := r.URL.Query()
	page, err := positiveQuery(query.Get("page"), 1)
	if err != nil {
		return 0, 0, fieldError(fieldPage, "must be an integer of at least 1")
	}
	pageSize, err := positiveQuery(query.Get("pageSize"), defaultPageSize)
	if err != nil {
		return 0, 0, fieldError(fieldPageSize, "must be an integer between 1 and 100")
	}
	if pageSize > maxPageSize {
		return 0, 0, fieldError(fieldPageSize, "must be an integer between 1 and 100")
	}
	return page, pageSize, nil
}

func positiveQuery(raw string, fallback int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("not an integer")
	}
	if parsed < 1 {
		return 0, errors.New("below the lower bound")
	}
	return parsed, nil
}

// GetProfile returns the signed-in customer's profile. It answers from the stored
// reference alone, so a media outage can never fail the read (FR-021).
func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.sessionAccount(w, r)
	if !ok {
		return
	}
	profile, err := h.svc.GetProfile(r.Context(), accountID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toProfileResponse(profile))
}

// UpdateProfile changes the display name and/or the phone.
func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.sessionAccount(w, r)
	if !ok {
		return
	}
	var req httpdto.UpdateProfileRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMalformedRequest), h.logger)
		return
	}
	phone, err := req.PhoneUpdate()
	if err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMalformedRequest), h.logger)
		return
	}
	profile, err := h.svc.UpdateProfile(r.Context(), appdto.UpdateProfileInput{
		UserID:      accountID,
		DisplayName: req.DisplayName,
		Phone:       phone,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toProfileResponse(profile))
}

// SetAvatar stores an uploaded avatar. The bytes are read with a hard ceiling and
// handed to the use case, which validates them by content; neither the file name
// nor the client-declared media type is trusted (FR-014, research D7).
func (h *Handler) SetAvatar(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.sessionAccount(w, r)
	if !ok {
		return
	}
	content, appErr := h.readAvatarFile(r)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	profile, err := h.svc.SetAvatar(r.Context(), appdto.SetAvatarInput{
		UserID:  accountID,
		Content: content,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toProfileResponse(profile))
}

// readAvatarFile streams the "file" part of the multipart body, refusing to
// buffer more than the configured ceiling. Reading with a ceiling is what makes
// the size rule enforceable: a post-hoc check on an already buffered body cannot
// stop a memory-exhaustion upload.
//
// The client-declared file name is deliberately not returned: the domain decides
// the image type from the leading signature, so a name has no reader anywhere
// below this line and carrying it further would only invite trusting it (FR-014,
// research D7).
//
// The ceiling is applied twice, on purpose. The route's middleware refuses a body
// whose declared length is already over the limit, and the LimitReader below stops
// a body that lies about its length or arrives in chunks. The second case is the
// one that matters for an attacker: a declared length cannot be trusted, and the
// route-level wrapper would otherwise surface as a parse failure rather than as the
// size rule it actually is.
func (h *Handler) readAvatarFile(r *http.Request) ([]byte, *httpx.AppError) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, fieldError(fieldFile, "a multipart/form-data body with a file part is required")
	}
	ceiling := h.cfg.AvatarMaxBytesOrDefault()
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			// The route's own body ceiling cuts the stream here when the declared
			// length was absent or understated.
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return nil, avatarTooLarge()
			}
			return nil, httpx.New(httpx.CodeMalformedRequest)
		}
		if part.FormName() != fieldFile {
			_ = part.Close()
			continue
		}
		// One extra byte distinguishes "at the ceiling" from "over it".
		content, err := io.ReadAll(io.LimitReader(part, ceiling+1))
		_ = part.Close()
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return nil, avatarTooLarge()
			}
			return nil, httpx.New(httpx.CodeMalformedRequest)
		}
		if int64(len(content)) > ceiling {
			return nil, avatarTooLarge()
		}
		return content, nil
	}
	return nil, fieldError(fieldFile, "a file part is required")
}

// avatarTooLarge is the one refusal for an upload over the ceiling, wherever the
// ceiling was reached: while reading the part, or by the route's own body limit.
// Naming the file in the detail is what lets a client point at the right input
// (FR-020).
func avatarTooLarge() *httpx.AppError {
	return withField(
		coded(constant.CodeAvatarTooLarge, http.StatusRequestEntityTooLarge, "Image exceeds the size limit"),
		fieldFile, "compress the image before uploading",
	)
}

// RemoveAvatar releases the stored avatar reference.
func (h *Handler) RemoveAvatar(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.sessionAccount(w, r)
	if !ok {
		return
	}
	profile, err := h.svc.RemoveAvatar(r.Context(), accountID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toProfileResponse(profile))
}

// ListAddresses returns a page of the account's non-hidden addresses.
func (h *Handler) ListAddresses(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.sessionAccount(w, r)
	if !ok {
		return
	}
	page, pageSize, appErr := pageParams(r)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	out, err := h.svc.ListAddresses(r.Context(), appdto.ListAddressesInput{
		UserID:   accountID,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		h.failAddress(w, r, err)
		return
	}
	httpx.WriteSuccessList(w, r, toAddressResponses(out.Addresses), out.Page, out.PageSize, out.Total)
}

// CreateAddress stores a new address. Required members are checked here so a
// client gets the offending field named; semantic validation — phone shape,
// province and ward existence, ward/province consistency — belongs to the use
// case and comes back through the module error codes.
func (h *Handler) CreateAddress(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.sessionAccount(w, r)
	if !ok {
		return
	}
	var req httpdto.CreateAddressRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMalformedRequest), h.logger)
		return
	}
	for _, required := range []struct {
		field string
		value string
	}{
		{fieldRecipientName, req.RecipientName},
		{fieldRecipient, req.RecipientPhone},
		{fieldProvinceCode, req.ProvinceCode},
		{fieldWardCode, req.WardCode},
		{fieldStreet, req.StreetAddress},
	} {
		if strings.TrimSpace(required.value) == "" {
			httpx.WriteError(w, r, fieldError(required.field, "is required"), h.logger)
			return
		}
	}
	address, err := h.svc.CreateAddress(r.Context(), appdto.CreateAddressInput{
		UserID:         accountID,
		RecipientName:  req.RecipientName,
		RecipientPhone: req.RecipientPhone,
		ProvinceCode:   req.ProvinceCode,
		ProvinceName:   req.ProvinceName,
		WardCode:       req.WardCode,
		WardName:       req.WardName,
		StreetAddress:  req.StreetAddress,
	})
	if err != nil {
		h.failAddress(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusCreated, toAddressResponse(address))
}

// UpdateAddress edits an address and preserves its default flag.
func (h *Handler) UpdateAddress(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.sessionAccount(w, r)
	if !ok {
		return
	}
	addressID, appErr := pathUUID(r, "addressId", fieldAddressID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	var req httpdto.UpdateAddressRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeMalformedRequest), h.logger)
		return
	}
	address, err := h.svc.UpdateAddress(r.Context(), appdto.UpdateAddressInput{
		UserID:         accountID,
		AddressID:      addressID,
		RecipientName:  req.RecipientName,
		RecipientPhone: req.RecipientPhone,
		ProvinceCode:   req.ProvinceCode,
		ProvinceName:   req.ProvinceName,
		WardCode:       req.WardCode,
		WardName:       req.WardName,
		StreetAddress:  req.StreetAddress,
	})
	if err != nil {
		h.failAddress(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toAddressResponse(address))
}

// DeleteAddress hides an address. The row survives so past orders keep the
// address text they used.
func (h *Handler) DeleteAddress(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.sessionAccount(w, r)
	if !ok {
		return
	}
	addressID, appErr := pathUUID(r, "addressId", fieldAddressID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	if err := h.svc.DeleteAddress(r.Context(), appdto.AddressRefInput{UserID: accountID, AddressID: addressID}); err != nil {
		h.failAddress(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SetDefaultAddress makes one address the account's single default.
func (h *Handler) SetDefaultAddress(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.sessionAccount(w, r)
	if !ok {
		return
	}
	addressID, appErr := pathUUID(r, "addressId", fieldAddressID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	address, err := h.svc.SetDefaultAddress(r.Context(), appdto.AddressRefInput{UserID: accountID, AddressID: addressID})
	if err != nil {
		h.failAddress(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toAddressResponse(address))
}

// LookupCustomer is the read-only operator lookup. The route is guarded by the
// ADMIN role middleware, offers no write path for customer data, and the use
// case audits every successful read (FR-022, FR-022a).
//
// The path identifier names whose data is read; the session names who is asking.
// Handing the actor to the use case through the context is what keeps FR-022a
// satisfiable at all: the contract method takes only the subject, so the identity
// has to travel beside it (research D5).
func (h *Handler) LookupCustomer(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	userID, appErr := pathUUID(r, "userId", fieldUserID)
	if appErr != nil {
		httpx.WriteError(w, r, appErr, h.logger)
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	customer, err := h.svc.LookupCustomer(ctx, userID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toCustomerLookupResponse(customer))
}

func toProfileResponse(profile appdto.ProfileOutput) httpdto.ProfileResponse {
	return httpdto.ProfileResponse{
		ID:          profile.ID,
		Email:       profile.Email,
		Role:        profile.Role,
		DisplayName: profile.DisplayName,
		Phone:       profile.Phone,
		Avatar:      toAvatarResponse(profile.Avatar),
	}
}

func toAvatarResponse(avatar *appdto.AvatarOutput) *httpdto.AvatarResponse {
	if avatar == nil {
		return nil
	}
	return &httpdto.AvatarResponse{
		PublicID: avatar.PublicID,
		URL:      avatar.URL,
		Width:    avatar.Width,
		Height:   avatar.Height,
	}
}

// toAddressResponse maps one address of the customer's own view. It is the
// single conversion point for that view: every self-service address route reads
// its answer through it, so the shape a customer sees cannot drift per route.
func toAddressResponse(address appdto.AddressOutput) httpdto.AddressResponse {
	return httpdto.AddressResponse{
		ID:                  address.ID,
		RecipientName:       address.RecipientName,
		RecipientPhone:      address.RecipientPhone,
		ProvinceCode:        address.ProvinceCode,
		ProvinceName:        address.ProvinceName,
		WardCode:            address.WardCode,
		WardName:            address.WardName,
		StreetAddress:       address.StreetAddress,
		IsDefault:           address.IsDefault,
		DivisionNeedsReview: address.DivisionNeedsReview,
	}
}

// toAddressResponses maps the customer's own address list.
//
// It is the single conversion point for that view, and it always allocates: an
// account with no address answers with an empty array rather than null. The
// contract declares the list as an array, so a null would be a shape no client can
// read — the same reason the operator lookup's list allocates.
func toAddressResponses(list []appdto.AddressOutput) []httpdto.AddressResponse {
	out := make([]httpdto.AddressResponse, 0, len(list))
	for _, address := range list {
		out = append(out, toAddressResponse(address))
	}
	return out
}

func toCustomerLookupResponse(customer contracts.Customer) httpdto.CustomerLookupResponse {
	return httpdto.CustomerLookupResponse{
		ID:          customer.ID,
		Email:       customer.Email,
		Role:        customer.Role,
		DisplayName: customer.DisplayName,
		Phone:       customer.Phone,
		Addresses:   toCustomerAddressResponses(customer.Addresses),
	}
}

// toCustomerAddressResponses maps the operator view's addresses.
//
// This is the single conversion point for the operator view, built from the
// cross-module contract type. It targets OperatorAddressResponse rather than the
// customer's AddressResponse: the contract address carries no
// divisionNeedsReview, nothing on this path checks a stored code against the
// official dataset, and a shared member would have to either assert an
// unverified `false` or drop the very `false` the customer's own list needs to
// state (research D10).
//
// Unlike the customer's own listing, an account with no address answers with an
// empty array rather than null: the contract declares addresses as a required
// array, so a null would be a shape no client can read.
func toCustomerAddressResponses(list []contracts.CustomerAddress) []httpdto.OperatorAddressResponse {
	out := make([]httpdto.OperatorAddressResponse, 0, len(list))
	for _, address := range list {
		out = append(out, httpdto.OperatorAddressResponse{
			ID:             address.ID,
			RecipientName:  address.RecipientName,
			RecipientPhone: address.RecipientPhone,
			ProvinceCode:   address.ProvinceCode,
			ProvinceName:   address.ProvinceName,
			WardCode:       address.WardCode,
			WardName:       address.WardName,
			StreetAddress:  address.StreetAddress,
			IsDefault:      address.IsDefault,
		})
	}
	return out
}
