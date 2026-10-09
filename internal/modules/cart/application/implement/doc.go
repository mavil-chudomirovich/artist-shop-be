// Package implement holds the cart module's use cases: the reads and writes that
// a customer reaches through the /cart routes. Each runs with the acting customer
// taken from the session, never from a request, and each write runs inside the
// UnitOfWork transaction that locks the cart's row (research D8, D9).
package implement
