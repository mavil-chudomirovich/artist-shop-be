package httpx

// SwaggerSuccess documents the success envelope for the generated OpenAPI
// specification. The runtime envelope types (successResponse, errorResponse in
// response.go) are unexported, so annotation tooling cannot name them; these
// aliases mirror the exact wire shape and are referenced only from `@Success`
// and `@Failure` annotations.
//
// The `data` member is intentionally `any`: a handler overrides it per endpoint
// with the concrete payload, e.g.
// `@Success 200 {object} httpx.SwaggerSuccess{data=httpdto.ProfileResponse}`.
type SwaggerSuccess struct {
	Data any  `json:"data"`
	Meta Meta `json:"meta"`
}

// SwaggerError documents the error envelope for the generated specification.
type SwaggerError struct {
	Error ErrorBody `json:"error"`
}
