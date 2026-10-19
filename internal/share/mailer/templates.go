package mailer

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"strconv"
	"strings"
	"sync"
)

//go:embed templates/*.html
var templatesFS embed.FS

// EmailFunc represents the function/purpose of the email being sent.
type EmailFunc string

// The email functions identify which template and subject a message uses.
const (
	FuncRegisterOTP          EmailFunc = "register_otp"
	FuncPasswordResetOTP     EmailFunc = "password_reset_otp"
	FuncOrderStatusUpdate    EmailFunc = "order_status_update"
	FuncArtistOrderConfirm   EmailFunc = "artist_order_confirm"
	FuncArtistFeedbackNotify EmailFunc = "artist_feedback_notify"
	FuncAdminPaymentReceived EmailFunc = "admin_payment_received"
)

// ShortOrderID returns the display form of an order identifier: only the part
// before the first dash (e.g. "fbfec5c1" for
// "fbfec5c1-7f15-406f-b74a-ae1f5b26e420"). The full identifier stays in the
// email links; only the visible code is shortened.
func ShortOrderID(orderID string) string {
	if i := strings.IndexByte(orderID, '-'); i >= 0 {
		return orderID[:i]
	}
	return orderID
}

// DefaultAppName is the fallback app name if APP_NAME env is unset.
const DefaultAppName = "Sumichi Art Shop"

// RegisterOTPParams holds real inputs for the registration verification email.
type RegisterOTPParams struct {
	AppName          string
	DisplayName      string
	Email            string
	OTP              string
	ExpiresInMinutes int
	SupportEmail     string
}

// PasswordResetOTPParams holds real inputs for the password reset email.
type PasswordResetOTPParams struct {
	AppName          string
	DisplayName      string
	Email            string
	OTP              string
	ExpiresInMinutes int
	SupportEmail     string
}

// OrderLineItem represents one product line in an order.
type OrderLineItem struct {
	ProductName string
	VariantName string
	Quantity    int64
	LineTotal   string
}

// OrderStatusParams holds real inputs for the order status update email.
type OrderStatusParams struct {
	AppName        string
	CustomerName   string
	OrderID        string
	RawStatus      string // Technical status code (e.g. "PAID", "SHIPPING"), mapped to friendly text
	OrderDate      string
	OrderDateLabel string // Optional: the meaning of OrderDate, e.g. "Ngày đặt" or "Cập nhật"
	Lines          []OrderLineItem
	TotalAmount    string
	OrderURL       string // Real frontend URL (e.g. http://localhost:3000/orders/{id})
	Note           string
	SupportEmail   string // Real support email from AdminEmail
}

// orderStatusView is the rendered shape of an order status email: the caller's
// facts plus the friendly copy and the safe tone class derived from RawStatus.
type orderStatusView struct {
	OrderStatusParams
	StatusTitle   string
	StatusBadge   string
	StatusTone    string
	StatusMessage string
	Preheader     string
	OrderShortID  string
}

// ArtistOrderConfirmParams holds real inputs for notifying the artist of a new order.
type ArtistOrderConfirmParams struct {
	AppName      string
	ArtistName   string
	OrderID      string
	OrderTime    string
	CustomerName string
	ItemsSummary string
	TotalAmount  string
	ConfirmURL   string
	SupportEmail string
	// OrderShortID is the visible display form of OrderID. Renderers fill it
	// from OrderID; callers leave it empty.
	OrderShortID string
}

// ArtistFeedbackParams holds real inputs for notifying the artist of customer feedback.
type ArtistFeedbackParams struct {
	AppName         string
	ArtistName      string
	OrderID         string
	CustomerName    string
	FeedbackContent string
	FeedbackDate    string
	OrderURL        string
	SupportEmail    string
	// OrderShortID is the visible display form of OrderID. Renderers fill it
	// from OrderID; callers leave it empty.
	OrderShortID string
}

// AdminPaymentParams holds real inputs for notifying the shop operator that a
// customer paid an order.
type AdminPaymentParams struct {
	AppName       string
	AdminName     string
	OrderID       string
	CustomerName  string
	CustomerEmail string
	PaidAt        string
	Lines         []OrderLineItem
	TotalAmount   string
	OrderURL      string
	SupportEmail  string
	// OrderShortID is the visible display form of OrderID. Renderers fill it
	// from OrderID; callers leave it empty.
	OrderShortID string
}

var (
	templatesOnce   sync.Once
	parsedTemplates *template.Template
	initErr         error
)

func getTemplates() (*template.Template, error) {
	templatesOnce.Do(func() {
		parsedTemplates, initErr = template.ParseFS(templatesFS, "templates/*.html")
	})
	return parsedTemplates, initErr
}

// FormatMoney formats an integer minor unit amount and currency code for user display.
// For VND, it outputs with dot thousand separators (e.g. 1500000 -> "1.500.000 ₫").
func FormatMoney(amount int64, currency string) string {
	curr := strings.ToUpper(strings.TrimSpace(currency))
	if curr == "" || curr == "VND" {
		sign := ""
		if amount < 0 {
			sign = "-"
			amount = -amount
		}
		str := strconv.FormatInt(amount, 10)
		n := len(str)
		if n <= 3 {
			return sign + str + " ₫"
		}
		var b strings.Builder
		b.WriteString(sign)
		rem := n % 3
		if rem > 0 {
			b.WriteString(str[:rem])
		}
		for i := rem; i < n; i += 3 {
			if b.Len() > len(sign) {
				b.WriteString(".")
			}
			b.WriteString(str[i : i+3])
		}
		b.WriteString(" ₫")
		return b.String()
	}
	return fmt.Sprintf("%d %s", amount, curr)
}

// HumanizeOrderStatus maps the order module's seven technical status codes to
// friendly, end-user text and a safe CSS tone. It never returns the raw code.
func HumanizeOrderStatus(rawStatus string) (badge, tone, title, message string) {
	normalized := strings.ToUpper(strings.TrimSpace(rawStatus))
	switch normalized {
	case "PENDING":
		return "Chờ xác nhận", "pending",
			"Đơn hàng đang chờ xác nhận",
			"Đơn hàng của bạn đã được tiếp nhận và đang chờ kiểm tra, xác nhận."
	case "PAYMENT_PENDING":
		return "Chờ thanh toán", "payment",
			"Đơn hàng đang chờ thanh toán",
			"Đơn hàng đã được xác nhận. Vui lòng hoàn tất thanh toán để đơn hàng bắt đầu được chuẩn bị."
	case "PAID":
		return "Đã thanh toán", "paid",
			"Thanh toán thành công",
			"Chúng tôi đã nhận được thanh toán cho đơn hàng. Tác phẩm sẽ sớm được chuẩn bị và giao đến bạn."
	case "SHIPPING":
		return "Đang vận chuyển", "shipping",
			"Tác phẩm đang trên đường giao đến bạn!",
			"Kiện hàng mang tác phẩm nghệ thuật độc bản đã được bàn giao cho đơn vị vận chuyển và đang trên đường đến với bạn."
	case "DELIVERED":
		return "Đã giao thành công", "delivered",
			"Đơn hàng đã được giao thành công!",
			"Tác phẩm đã đến tay bạn an toàn. Hãy mở kiện hàng, kiểm tra và gửi đánh giá về đơn hàng nhé!"
	case "COMPLETED":
		return "Đơn hàng hoàn tất", "completed",
			"Đơn hàng đã hoàn tất",
			"Cảm ơn bạn đã tin tưởng mua sắm và ủng hộ nghệ thuật. Chúc bạn có những phút giây trọn vẹn bên tác phẩm!"
	case "CANCELLED":
		return "Đơn hàng đã hủy", "cancelled",
			"Thông báo hủy đơn hàng",
			"Đơn hàng của bạn đã được hủy theo yêu cầu hoặc do quá thời hạn thanh toán."
	default:
		return "Đang xử lý", "default",
			"Cập nhật thông tin đơn hàng",
			"Đơn hàng của bạn đang được xử lý trong hệ thống."
	}
}

// RenderRegisterOTPEmail produces the subject and HTML body for account verification.
func RenderRegisterOTPEmail(params RegisterOTPParams) (subject, htmlBody string, err error) {
	tmpl, err := getTemplates()
	if err != nil {
		return "", "", fmt.Errorf("load email templates: %w", err)
	}

	if params.AppName == "" {
		params.AppName = DefaultAppName
	}
	if params.ExpiresInMinutes <= 0 {
		params.ExpiresInMinutes = 10
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "register_otp.html", params); err != nil {
		return "", "", fmt.Errorf("render register_otp template: %w", err)
	}

	subject = fmt.Sprintf("[%s] Mã xác thực đăng ký tài khoản", params.AppName)
	return subject, buf.String(), nil
}

// RenderPasswordResetOTPEmail produces the subject and HTML body for password reset.
func RenderPasswordResetOTPEmail(params PasswordResetOTPParams) (subject, htmlBody string, err error) {
	tmpl, err := getTemplates()
	if err != nil {
		return "", "", fmt.Errorf("load email templates: %w", err)
	}

	if params.AppName == "" {
		params.AppName = DefaultAppName
	}
	if params.ExpiresInMinutes <= 0 {
		params.ExpiresInMinutes = 10
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "password_reset_otp.html", params); err != nil {
		return "", "", fmt.Errorf("render password_reset_otp template: %w", err)
	}

	subject = fmt.Sprintf("[%s] Yêu cầu đặt lại mật khẩu", params.AppName)
	return subject, buf.String(), nil
}

// RenderOrderStatusEmail produces the subject and HTML body for customer order status changes.
func RenderOrderStatusEmail(params OrderStatusParams) (subject, htmlBody string, err error) {
	tmpl, err := getTemplates()
	if err != nil {
		return "", "", fmt.Errorf("load email templates: %w", err)
	}

	if params.AppName == "" {
		params.AppName = DefaultAppName
	}

	badge, tone, title, message := HumanizeOrderStatus(params.RawStatus)
	if params.OrderDateLabel == "" {
		params.OrderDateLabel = "Cập nhật"
	}
	shortID := ShortOrderID(params.OrderID)
	view := orderStatusView{
		OrderStatusParams: params,
		StatusTitle:       title,
		StatusBadge:       badge,
		StatusTone:        tone,
		StatusMessage:     message,
		Preheader:         fmt.Sprintf("%s - Đơn hàng #%s", title, shortID),
		OrderShortID:      shortID,
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "order_status_update.html", view); err != nil {
		return "", "", fmt.Errorf("render order_status_update template: %w", err)
	}

	subject = fmt.Sprintf("[%s] %s - Đơn hàng #%s", params.AppName, title, shortID)
	return subject, buf.String(), nil
}

// RenderArtistOrderConfirmEmail produces the subject and HTML body notifying the artist of a new order.
func RenderArtistOrderConfirmEmail(params ArtistOrderConfirmParams) (subject, htmlBody string, err error) {
	tmpl, err := getTemplates()
	if err != nil {
		return "", "", fmt.Errorf("load email templates: %w", err)
	}

	if params.AppName == "" {
		params.AppName = DefaultAppName
	}
	params.OrderShortID = ShortOrderID(params.OrderID)

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "order_artist_confirmation.html", params); err != nil {
		return "", "", fmt.Errorf("render order_artist_confirmation template: %w", err)
	}

	subject = fmt.Sprintf("[%s] Đơn hàng mới cần xác nhận - Đơn hàng #%s", params.AppName, params.OrderShortID)
	return subject, buf.String(), nil
}

// RenderArtistFeedbackEmail produces the subject and HTML body notifying the artist of received feedback.
func RenderArtistFeedbackEmail(params ArtistFeedbackParams) (subject, htmlBody string, err error) {
	tmpl, err := getTemplates()
	if err != nil {
		return "", "", fmt.Errorf("load email templates: %w", err)
	}

	if params.AppName == "" {
		params.AppName = DefaultAppName
	}
	params.OrderShortID = ShortOrderID(params.OrderID)

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "order_artist_feedback.html", params); err != nil {
		return "", "", fmt.Errorf("render order_artist_feedback template: %w", err)
	}

	subject = fmt.Sprintf("[%s] Đánh giá mới cho đơn hàng #%s", params.AppName, params.OrderShortID)
	return subject, buf.String(), nil
}

// RenderAdminPaymentReceivedEmail produces the subject and HTML body notifying
// the shop operator that a customer paid an order.
func RenderAdminPaymentReceivedEmail(params AdminPaymentParams) (subject, htmlBody string, err error) {
	tmpl, err := getTemplates()
	if err != nil {
		return "", "", fmt.Errorf("load email templates: %w", err)
	}

	if params.AppName == "" {
		params.AppName = DefaultAppName
	}
	params.OrderShortID = ShortOrderID(params.OrderID)

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "admin_payment_received.html", params); err != nil {
		return "", "", fmt.Errorf("render admin_payment_received template: %w", err)
	}

	subject = fmt.Sprintf("[%s] Khách hàng đã thanh toán - Đơn hàng #%s", params.AppName, params.OrderShortID)
	return subject, buf.String(), nil
}

// RenderEmailByFunc renders the appropriate email subject and HTML body based on the specified EmailFunc.
func RenderEmailByFunc(fn EmailFunc, data any) (subject, htmlBody string, err error) {
	switch fn {
	case FuncRegisterOTP:
		if p, ok := data.(RegisterOTPParams); ok {
			return RenderRegisterOTPEmail(p)
		}
		return "", "", fmt.Errorf("invalid data type for %s: expected RegisterOTPParams", fn)

	case FuncPasswordResetOTP:
		if p, ok := data.(PasswordResetOTPParams); ok {
			return RenderPasswordResetOTPEmail(p)
		}
		return "", "", fmt.Errorf("invalid data type for %s: expected PasswordResetOTPParams", fn)

	case FuncOrderStatusUpdate:
		if p, ok := data.(OrderStatusParams); ok {
			return RenderOrderStatusEmail(p)
		}
		return "", "", fmt.Errorf("invalid data type for %s: expected OrderStatusParams", fn)

	case FuncArtistOrderConfirm:
		if p, ok := data.(ArtistOrderConfirmParams); ok {
			return RenderArtistOrderConfirmEmail(p)
		}
		return "", "", fmt.Errorf("invalid data type for %s: expected ArtistOrderConfirmParams", fn)

	case FuncArtistFeedbackNotify:
		if p, ok := data.(ArtistFeedbackParams); ok {
			return RenderArtistFeedbackEmail(p)
		}
		return "", "", fmt.Errorf("invalid data type for %s: expected ArtistFeedbackParams", fn)

	case FuncAdminPaymentReceived:
		if p, ok := data.(AdminPaymentParams); ok {
			return RenderAdminPaymentReceivedEmail(p)
		}
		return "", "", fmt.Errorf("invalid data type for %s: expected AdminPaymentParams", fn)

	default:
		return "", "", fmt.Errorf("unsupported email function: %s", fn)
	}
}
