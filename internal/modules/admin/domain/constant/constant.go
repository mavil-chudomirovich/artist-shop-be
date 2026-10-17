package constant

// The admin dashboard's request defaults and bounds. These are the values the
// handler validates against and the application echoes, so they live in one place
// the presentation and application layers can both name without importing each
// other (FR-003, FR-004).
const (
	// DefaultDays is the revenue-trend length when the request names none.
	DefaultDays = 7
	// DefaultLowStockThreshold is the inventory warning threshold when the
	// request names none.
	DefaultLowStockThreshold int64 = 5
	// MinLowStockThreshold is the smallest accepted threshold.
	MinLowStockThreshold int64 = 0
	// MaxLowStockThreshold is the largest accepted threshold.
	MaxLowStockThreshold int64 = 10000
)

// AllowedDays is the closed set of accepted trend lengths (FR-003).
var AllowedDays = []int{7, 30}

// RecentOrderLimit is how many newest orders the dashboard shows. The contract
// fixes it at five; it is not a query parameter (FR-025, research D10).
const RecentOrderLimit = 5

// The audit action and target of a successful dashboard view. They are part of the
// audit contract, so changing a value makes historical rows undecodable
// (FR-030, research D11).
const (
	// AuditDashboardViewed is recorded once per successful dashboard request.
	AuditDashboardViewed = "ADMIN_DASHBOARD_VIEWED"
	// TargetTypeDashboard is the audit target type of the view.
	TargetTypeDashboard = "dashboard"
	// TargetIDOverview is the audit target id of the overview endpoint.
	TargetIDOverview = "overview"
)
