package mailer

import (
	"strings"
	"testing"
)

func TestFormatMoney(t *testing.T) {
	tests := []struct {
		amount   int64
		currency string
		want     string
	}{
		{0, "VND", "0 ₫"},
		{500, "VND", "500 ₫"},
		{1000, "VND", "1.000 ₫"},
		{125000, "VND", "125.000 ₫"},
		{2500000, "VND", "2.500.000 ₫"},
		{-50000, "VND", "-50.000 ₫"},
		{100, "USD", "100 USD"},
	}

	for _, tt := range tests {
		got := FormatMoney(tt.amount, tt.currency)
		if got != tt.want {
			t.Errorf("FormatMoney(%d, %q) = %q, want %q", tt.amount, tt.currency, got, tt.want)
		}
	}
}

func TestRenderRegisterOTPEmail(t *testing.T) {
	subject, html, err := RenderRegisterOTPEmail(RegisterOTPParams{
		DisplayName:      "Nguyễn Văn A",
		Email:            "a@example.com",
		OTP:              "948215",
		ExpiresInMinutes: 15,
		SupportEmail:     "admin@artist-shop.local",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(subject, "Mã xác thực đăng ký tài khoản") {
		t.Errorf("subject missing expected text: %s", subject)
	}
	if !strings.Contains(html, "948215") {
		t.Errorf("html body missing OTP: %s", html)
	}
	if !strings.Contains(html, "Nguyễn Văn A") {
		t.Errorf("html body missing DisplayName: %s", html)
	}
	if !strings.Contains(html, "a@example.com") {
		t.Errorf("html body missing recipient email: %s", html)
	}
	if !strings.Contains(html, "15 phút") {
		t.Errorf("html body missing expiration: %s", html)
	}
	if !strings.Contains(html, "admin@artist-shop.local") {
		t.Errorf("html body missing real support email: %s", html)
	}
}

func TestRenderPasswordResetOTPEmail(t *testing.T) {
	subject, html, err := RenderPasswordResetOTPEmail(PasswordResetOTPParams{
		Email:            "user@example.com",
		OTP:              "123456",
		ExpiresInMinutes: 10,
		SupportEmail:     "admin@artist-shop.local",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(subject, "Yêu cầu đặt lại mật khẩu") {
		t.Errorf("subject missing expected text: %s", subject)
	}
	if !strings.Contains(html, "123456") {
		t.Errorf("html body missing OTP: %s", html)
	}
	if !strings.Contains(html, "Bảo Mật Tài Khoản") {
		t.Errorf("html body missing header badge: %s", html)
	}
	if !strings.Contains(html, "admin@artist-shop.local") {
		t.Errorf("html body missing real support email: %s", html)
	}
}

func TestRenderOrderStatusEmail(t *testing.T) {
	tests := []struct {
		name      string
		rawStatus string
		wantBadge string
		wantTone  string
	}{
		{name: "Pending Status", rawStatus: "PENDING", wantBadge: "Chờ xác nhận", wantTone: "pending"},
		{name: "Payment Pending Status", rawStatus: "PAYMENT_PENDING", wantBadge: "Chờ thanh toán", wantTone: "payment"},
		{name: "Paid Status", rawStatus: "PAID", wantBadge: "Đã thanh toán", wantTone: "paid"},
		{name: "Shipping Status", rawStatus: "SHIPPING", wantBadge: "Đang vận chuyển", wantTone: "shipping"},
		{name: "Delivered Status", rawStatus: "DELIVERED", wantBadge: "Đã giao thành công", wantTone: "delivered"},
		{name: "Completed Status", rawStatus: "COMPLETED", wantBadge: "Đơn hàng hoàn tất", wantTone: "completed"},
		{name: "Cancelled Status", rawStatus: "CANCELLED", wantBadge: "Đơn hàng đã hủy", wantTone: "cancelled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subject, html, err := RenderOrderStatusEmail(OrderStatusParams{
				CustomerName: "Trần Thị B",
				OrderID:      "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
				RawStatus:    tt.rawStatus,
				TotalAmount:  FormatMoney(1250000, "VND"),
				OrderURL:     "http://localhost:3000/account/orders/9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
				SupportEmail: "admin@artist-shop.local",
				Lines: []OrderLineItem{
					{
						ProductName: "Tranh sơn dầu Phố Cổ",
						VariantName: "Khung gỗ sồi 60x80cm",
						Quantity:    1,
						LineTotal:   FormatMoney(1250000, "VND"),
					},
				},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !strings.Contains(subject, "#9b1deb4d") {
				t.Errorf("subject missing order ID: %s", subject)
			}
			if !strings.Contains(html, "#9b1deb4d") {
				t.Errorf("html body missing order ID badge: %s", html)
			}
			if !strings.Contains(html, tt.wantBadge) {
				t.Errorf("html body missing friendly badge %q: %s", tt.wantBadge, html)
			}
			if !strings.Contains(html, "order-badge-"+tt.wantTone) {
				t.Errorf("html body missing safe tone class %q: %s", tt.wantTone, html)
			}
			if strings.Contains(html, "ZgotmplZ") {
				t.Errorf("html body contains an unsafe CSS placeholder: %s", html)
			}
			if strings.Contains(html, tt.rawStatus) {
				t.Errorf("html body must not contain the raw status %q", tt.rawStatus)
			}
			if !strings.Contains(html, "1.250.000 ₫") {
				t.Errorf("html body missing formatted amount: %s", html)
			}
			if !strings.Contains(html, "Tranh sơn dầu Phố Cổ") {
				t.Errorf("html body missing product line item: %s", html)
			}
			if !strings.Contains(html, "http://localhost:3000/account/orders/9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d") {
				t.Errorf("html body missing real order url: %s", html)
			}
		})
	}
}

func TestRenderArtistOrderConfirmEmail(t *testing.T) {
	subject, html, err := RenderArtistOrderConfirmEmail(ArtistOrderConfirmParams{
		ArtistName:   "Họa Sĩ Lê Minh",
		OrderID:      "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
		OrderTime:    "19/10/2026 21:30",
		CustomerName: "Phạm Hải",
		ItemsSummary: "1x Tranh sơn dầu 'Hoàng Hôn Trên Biển' (Size 60x80cm)",
		TotalAmount:  FormatMoney(3500000, "VND"),
		ConfirmURL:   "http://localhost:3000/admin/orders/9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
		SupportEmail: "admin@artist-shop.local",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(subject, "#9b1deb4d") {
		t.Errorf("subject missing order ID: %s", subject)
	}
	if !strings.Contains(html, "#9b1deb4d") {
		t.Errorf("html body missing order ID: %s", html)
	}
	if !strings.Contains(html, "Họa Sĩ Lê Minh") {
		t.Errorf("html body missing artist name: %s", html)
	}
	if !strings.Contains(html, "Hoàng Hôn Trên Biển") {
		t.Errorf("html body missing items summary: %s", html)
	}
	if !strings.Contains(html, "Chờ xác nhận") {
		t.Errorf("html body missing friendly status: %s", html)
	}
	if strings.Contains(html, "PENDING") {
		t.Errorf("html body must not contain the raw status: %s", html)
	}
}

func TestRenderArtistFeedbackEmail(t *testing.T) {
	subject, html, err := RenderArtistFeedbackEmail(ArtistFeedbackParams{
		ArtistName:      "Họa Sĩ Lê Minh",
		OrderID:         "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
		CustomerName:    "Phạm Hải",
		FeedbackContent: "Tranh nhận được rất đẹp, đóng gói cẩn thận từng góc cạnh!",
		FeedbackDate:    "19/10/2026 22:15",
		OrderURL:        "http://localhost:3000/admin/orders/9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
		SupportEmail:    "admin@artist-shop.local",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(subject, "#9b1deb4d") {
		t.Errorf("subject missing order ID: %s", subject)
	}
	if !strings.Contains(html, "#9b1deb4d") {
		t.Errorf("html body missing order ID: %s", html)
	}
	if !strings.Contains(html, "Tranh nhận được rất đẹp") {
		t.Errorf("html body missing feedback content: %s", html)
	}
}

func TestRenderEmailByFunc(t *testing.T) {
	subject, html, err := RenderEmailByFunc(FuncOrderStatusUpdate, OrderStatusParams{
		OrderID:   "TEST123",
		RawStatus: "SHIPPING",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(subject, "#TEST123") {
		t.Errorf("subject missing order ID: %s", subject)
	}
	if !strings.Contains(html, "Đang vận chuyển") {
		t.Errorf("html missing friendly status text: %s", html)
	}

	// Test invalid func
	_, _, err = RenderEmailByFunc(EmailFunc("unknown"), nil)
	if err == nil {
		t.Errorf("expected error for unknown func")
	}
}

func TestShortOrderID(t *testing.T) {
	if got := ShortOrderID("fbfec5c1-7f15-406f-b74a-ae1f5b26e420"); got != "fbfec5c1" {
		t.Errorf("ShortOrderID = %q, want %q", got, "fbfec5c1")
	}
	if got := ShortOrderID("no-dash"); got != "no" {
		t.Errorf("ShortOrderID = %q, want %q", got, "no")
	}
	if got := ShortOrderID("plain"); got != "plain" {
		t.Errorf("ShortOrderID = %q, want %q", got, "plain")
	}
}

func TestRenderAdminPaymentReceivedEmail(t *testing.T) {
	subject, html, err := RenderAdminPaymentReceivedEmail(AdminPaymentParams{
		OrderID:       "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
		CustomerName:  "Phạm Hải",
		CustomerEmail: "hai@example.com",
		PaidAt:        "19/10/2026 21:30",
		TotalAmount:   FormatMoney(3500000, "VND"),
		OrderURL:      "http://localhost:3000/admin/orders/9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
		SupportEmail:  "admin@artist-shop.local",
		Lines: []OrderLineItem{
			{
				ProductName: "Tranh sơn dầu Hoàng Hôn Trên Biển",
				Quantity:    1,
				LineTotal:   FormatMoney(3500000, "VND"),
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(subject, "#9b1deb4d") {
		t.Errorf("subject missing short order ID: %s", subject)
	}
	if strings.Contains(subject, "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d") {
		t.Errorf("subject must not contain the full order ID: %s", subject)
	}
	if !strings.Contains(html, "#9b1deb4d") {
		t.Errorf("html body missing short order ID: %s", html)
	}
	if !strings.Contains(html, "Đã thanh toán") {
		t.Errorf("html body missing payment badge: %s", html)
	}
	if !strings.Contains(html, "http://localhost:3000/admin/orders/9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d") {
		t.Errorf("html body missing admin order url: %s", html)
	}
	if !strings.Contains(html, "Phạm Hải") {
		t.Errorf("html body missing customer name: %s", html)
	}
}
