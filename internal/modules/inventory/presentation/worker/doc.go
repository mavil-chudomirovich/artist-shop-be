// Package worker hosts the inventory module's background sweeper, the loop that
// releases expired holds. It carries no business rule of its own: it calls an
// ordinary application use case every interval and stops on context cancellation
// (research D6).
package worker
