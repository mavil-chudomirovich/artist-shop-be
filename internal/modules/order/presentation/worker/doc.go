// Package worker hosts the order module's background sweeper, the loop that
// cancels unpaid orders past their expiry and returns their goods. It carries no
// business rule of its own: it calls an ordinary application use case every
// interval and stops on context cancellation (FR-012, research D6).
package worker
