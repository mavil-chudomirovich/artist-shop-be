package media

import (
	"context"
	"crypto/sha1" //nolint:gosec // Cloudinary's signed API mandates SHA-1; it is a keyed authentication hash, not a content hash.
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/logging"
)

// DefaultBaseURL is Cloudinary's signed REST API root.
const DefaultBaseURL = "https://api.cloudinary.com"

// defaultTimeout bounds a provider call. The upload ceiling keeps the request
// small, so anything slower than this is an outage rather than a slow upload, and
// a customer should not be left waiting for a default HTTP client timeout.
const defaultTimeout = 15 * time.Second

// maxResponseBytes caps how much of a provider answer is read. The four fields
// this adapter needs come in the first few hundred bytes, so a misbehaving or
// substituted endpoint cannot make the service buffer an arbitrary body.
const maxResponseBytes = 1 << 20

// The operation names used in this service's own log lines. They are fixed words,
// never the endpoint, so a log line cannot be turned back into a provider URL.
const (
	operationUpload = "upload"
	operationRemove = "remove"
)

// Cloudinary stores avatar bytes outside this service.
//
// It speaks Cloudinary's signed REST API directly with the standard library: the
// SDK would add a dependency for two endpoints, and the signed API is a signature
// over a sorted parameter list plus a secret (ADR-005 refuses new dependencies for
// this feature, and the media adapter is exactly where an SDK would otherwise
// arrive).
//
// Every provider failure — a transport error, a non-2xx status, a body that cannot
// be read or parsed, a success body missing a field this service needs — comes back
// as domainerr.ErrMediaUnavailable and nothing more. No provider prose, no
// response body, no URL and no credential is attached: presentation logs whatever
// error it is given, so anything carried here would reach a log line, and a log
// line is not a place for a provider's internal message or an internal hostname
// (Constitution V, VI). The HTTP status is the one piece of provider detail kept,
// because it is a number this service chose to branch on and contains nothing
// sensitive.
//
// Because that leaves an operator with nothing at all to look at, the adapter also
// logs its own sanitised classification of the failure, and only that: the same
// wording the caller would have seen, at a level that matches how bad the failure
// is. The classification is already free of provider detail, so the log line is a
// diagnosable trace of an outage without becoming a place secrets leak to.
type Cloudinary struct {
	cfg         config.MediaConfig
	client      *http.Client
	logger      *slog.Logger
	baseURL     string
	uploadPath  string
	destroyPath string
}

// New creates the Cloudinary adapter.
//
// baseURL is a parameter rather than a constant so a test can point the adapter at
// an httptest server; production wiring passes DefaultBaseURL. A nil client gets a
// client with a bounded timeout, and a nil logger falls back to the default one so
// the failure path can never be the thing that panics.
func New(cfg config.MediaConfig, client *http.Client, logger *slog.Logger, baseURL string) *Cloudinary {
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	if logger == nil {
		logger = slog.Default()
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Cloudinary{
		cfg:         cfg,
		client:      client,
		logger:      logger,
		baseURL:     strings.TrimSuffix(baseURL, "/"),
		uploadPath:  "/v1_1/{cloud}/image/upload",
		destroyPath: "/v1_1/{cloud}/image/destroy",
	}
}

// NewWithDefaults creates the adapter against the real Cloudinary API with a
// bounded HTTP client.
func NewWithDefaults(cfg config.MediaConfig, logger *slog.Logger) *Cloudinary {
	return New(cfg, nil, logger, DefaultBaseURL)
}

var _ appinterface.MediaStore = (*Cloudinary)(nil)

// Upload stores the original bytes and asks the provider to resize them to
// targetWidth as part of the upload (ADR-005, research D6). The provider, not this
// service, is the system of record for the resulting dimensions, so the answer it
// reports is what the caller stores.
//
// The original bytes go up untouched: this service adds no imaging dependency and
// never decodes a pixel. That is also why the resize cannot fail in a way this
// service could detect — a provider that ignores the transformation reports the
// original dimensions, and the domain's width guard refuses the reference rather
// than storing something the contract says cannot exist.
func (c *Cloudinary) Upload(ctx context.Context, content []byte, targetWidth int) (appinterface.MediaReference, error) {
	if err := c.requireCredentials(operationUpload); err != nil {
		return appinterface.MediaReference{}, err
	}

	form := url.Values{}
	form.Set("file", base64.StdEncoding.EncodeToString(content))
	form.Set("api_key", c.cfg.APIKey)
	form.Set("timestamp", strconv.FormatInt(time.Now().UTC().Unix(), 10))
	if c.cfg.Folder != "" {
		form.Set("folder", c.cfg.Folder)
	}
	if targetWidth > 0 {
		form.Set("transformation", resizeTransformation(targetWidth))
	}
	form.Set("signature", c.sign(signedParameters(form)))

	body, status, err := c.post(ctx, operationUpload, c.uploadPath, form)
	if err != nil {
		return appinterface.MediaReference{}, err
	}

	var answer uploadResponse
	if err := json.Unmarshal(body, &answer); err != nil {
		return appinterface.MediaReference{}, c.unavailable(ctx, operationUpload, slog.LevelError,
			fmt.Sprintf("the provider answer could not be read (status %d)", status))
	}
	if answer.PublicID == "" || answer.SecureURL == "" {
		return appinterface.MediaReference{}, c.unavailable(ctx, operationUpload, slog.LevelError,
			fmt.Sprintf("the provider answer carried no usable reference (status %d)", status))
	}
	return appinterface.MediaReference{
		PublicID: answer.PublicID,
		URL:      answer.SecureURL,
		Width:    answer.Width,
		Height:   answer.Height,
	}, nil
}

// Remove releases a stored reference.
//
// The port's contract is that releasing an unknown reference succeeds, so a retry
// never fails on an asset that is already gone: the provider answers a destroy of
// an unknown identifier with 200 and a "not found" result, which this method
// treats as success. Anything else is a genuine outage and is reported as one, but
// only after the row that referenced the asset has already been written — see
// implement.Service.releaseAvatar.
func (c *Cloudinary) Remove(ctx context.Context, ref appinterface.MediaReference) error {
	if err := c.requireCredentials(operationRemove); err != nil {
		return err
	}
	if strings.TrimSpace(ref.PublicID) == "" {
		// Nothing was ever stored, so nothing has to be released.
		return nil
	}

	form := url.Values{}
	form.Set("public_id", ref.PublicID)
	form.Set("api_key", c.cfg.APIKey)
	form.Set("timestamp", strconv.FormatInt(time.Now().UTC().Unix(), 10))
	form.Set("signature", c.sign(signedParameters(form)))

	if _, _, err := c.post(ctx, operationRemove, c.destroyPath, form); err != nil {
		return err
	}
	return nil
}

// unavailable records the sanitised classification of a provider failure and
// returns it as the module's retryable error, so the two can never drift apart:
// the operator and the caller are told the same thing, in the same words.
//
// The level follows the failure rather than being uniform. A refusal is a warning
// because the provider answered — an expired signature, a full quota or a rate
// limit is a condition a retry or a configuration change resolves — while an
// endpoint that cannot be reached and an answer this service cannot read are
// errors, because nothing about them is actionable without a fix.
//
// classification MUST be one of this adapter's own sentences: a status number, a
// fixed phrase. Nothing derived from the provider's body, the endpoint or the
// payload may be passed in, because this value is written to a log line.
func (c *Cloudinary) unavailable(ctx context.Context, operation string, level slog.Level, classification string) error {
	logging.WithCorrelation(ctx, c.logger).Log(ctx, level, "media provider call failed",
		slog.String("operation", operation),
		slog.String("classification", classification),
	)
	return fmt.Errorf("%w: %s", domainerr.ErrMediaUnavailable, classification)
}

// requireCredentials is the fail-closed gate.
//
// config.MediaConfig.IsConfigured() exists precisely for this: the media settings
// are absent from Validate and ValidateForAPI so that migrate and seed stay
// runnable without them, which means a deployment can legitimately reach the API
// with no media credentials at all. In that state the adapter refuses every call
// with the module's retryable error instead of inventing a reference or reporting
// a success — so a missing configuration disables avatar upload and nothing else
// (the profile read never calls the provider, FR-021).
//
// The message names the operation and the configuration key, never a value.
func (c *Cloudinary) requireCredentials(operation string) error {
	if c.cfg.IsConfigured() {
		return nil
	}
	return fmt.Errorf("%w: %s needs MEDIA_CLOUD_NAME, MEDIA_API_KEY and MEDIA_API_SECRET to be set",
		domainerr.ErrMediaUnavailable, operation)
}

// post sends one signed form to the provider and returns the raw body with the
// status it answered, or a bare media error that has already been logged.
func (c *Cloudinary) post(ctx context.Context, operation, path string, form url.Values) ([]byte, int, error) {
	endpoint := c.baseURL + strings.Replace(path, "{cloud}", url.PathEscape(c.cfg.CloudName), 1)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, c.unavailable(ctx, operation, slog.LevelError, "the provider request could not be built")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.client.Do(req)
	if err != nil {
		// The transport error is dropped rather than wrapped: net/http puts the
		// full endpoint URL, including the cloud name, into it.
		return nil, 0, c.unavailable(ctx, operation, slog.LevelError, "the provider could not be reached")
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, resp.StatusCode, c.unavailable(ctx, operation, slog.LevelError,
			fmt.Sprintf("the provider answer could not be read (status %d)", resp.StatusCode))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		// The provider's own error message is deliberately not read: it can name
		// the cloud, the account and the transformation, none of which belongs in
		// this service's logs or in a client response.
		return nil, resp.StatusCode, c.unavailable(ctx, operation, slog.LevelWarn,
			fmt.Sprintf("the provider refused the request (status %d)", resp.StatusCode))
	}
	return body, resp.StatusCode, nil
}

// signedParam is one provider parameter covered by the request signature.
type signedParam struct {
	key   string
	value string
}

// unsignedParameters are the parameters Cloudinary's signature does not cover:
// the payload itself, the values naming the cloud and the resource type, the api
// key, and the signature. Everything else sent — folder, timestamp,
// transformation, public_id — is signed, which is what stops a caller from adding
// an unsigned parameter that changes what the request does.
var unsignedParameters = map[string]bool{
	"file":          true,
	"cloud_name":    true,
	"resource_type": true,
	"api_key":       true,
	"signature":     true,
}

// signedParameters collects the parameters that go into the signature.
//
// It is a slice rather than a map so the signing rule and the request agree: the
// signature covers exactly what is sent, in the same values, and nothing is added
// to or dropped from one between the two.
func signedParameters(form url.Values) []signedParam {
	out := make([]signedParam, 0, len(form))
	for key := range form {
		if unsignedParameters[key] {
			continue
		}
		out = append(out, signedParam{key: key, value: form.Get(key)})
	}
	return out
}

// sign computes Cloudinary's request signature.
//
// The API specifies it as: take every parameter that is signed (which excludes the
// file itself and the api_key, and includes everything else), sort them by key,
// join "key=value" pairs with "&", append the API secret to that string, and take
// the SHA-1 of the result in lowercase hex.
//
// It is a keyed hash rather than an HMAC, which is a different algorithm with a
// different output, so the plain construction is the correct one here and the
// linter exception at the import records that the choice is the provider's.
func (c *Cloudinary) sign(params []signedParam) string {
	sorted := append([]signedParam(nil), params...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].key < sorted[j].key })

	var payload strings.Builder
	for i, param := range sorted {
		if i > 0 {
			payload.WriteByte('&')
		}
		payload.WriteString(param.key)
		payload.WriteByte('=')
		payload.WriteString(param.value)
	}
	payload.WriteString(c.cfg.APISecret)

	sum := sha1.Sum([]byte(payload.String()))
	return hex.EncodeToString(sum[:])
}

// resizeTransformation is the provider expression that downscales to the target
// width. Cloudinary's resize mode "limit" shrinks an image that is wider than the
// target and leaves a narrower one alone, which is what "a maximum width of 512
// pixels" means: a ceiling, not a forced size. Without "limit" a small photo would
// be stretched up to 512 px.
func resizeTransformation(targetWidth int) string {
	return fmt.Sprintf("c_limit,w_%d,q_auto", targetWidth)
}

// uploadResponse is the part of Cloudinary's answer this service stores. Nothing
// else the provider returns is kept: it would be provider detail with no use here
// and no place to put it.
type uploadResponse struct {
	PublicID  string `json:"public_id"`
	SecureURL string `json:"secure_url"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}
