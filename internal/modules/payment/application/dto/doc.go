// Package dto defines the payment module's use-case input/output types and the
// shapes the provider port exchanges. They are separate from the HTTP shapes:
// these carry what a use case computes, while presentation owns what a response
// looks like. Money is an integer amount in the currency's minor unit plus its
// currency, never a float (Constitution II).
package dto
