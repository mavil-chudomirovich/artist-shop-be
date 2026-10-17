// Package model holds the admin module's read models: the assembled dashboard
// overview, its KPI groups, the revenue trend, the category breakdown and the
// recent-order summaries, together with the pure time-window and growth helpers
// that shape them. The domain imports only the standard library, internal/share/access
// and github.com/google/uuid (Constitution I); the admin module owns no data and
// reads other modules only through internal/contracts.
package model
