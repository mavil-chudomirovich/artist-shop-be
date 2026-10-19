package mailer

import (
	"strings"
	"testing"
)

func TestBuildMessageEncodesVietnameseSubjectAndHTMLBody(t *testing.T) {
	message := string(BuildMessage("a@example.com", "shop@example.com",
		"[Sumichi Art Shop] Đơn hàng đã hoàn tất - Đơn hàng #42", "<!DOCTYPE html><html><body>Xin chào</body></html>"))

	if !strings.Contains(message, "Subject: =?utf-8?q?") {
		t.Fatalf("subject was not RFC 2047 encoded: %s", message)
	}
	if !strings.Contains(message, "Content-Type: text/html; charset=\"utf-8\"") {
		t.Fatalf("HTML body was not typed as HTML: %s", message)
	}
	if !strings.Contains(message, "Content-Transfer-Encoding: quoted-printable") {
		t.Fatalf("body was not quoted-printable encoded: %s", message)
	}
	if !strings.Contains(message, "Xin ch=C3=A0o") {
		t.Fatalf("the body was not encoded as quoted-printable: %s", message)
	}
}

func TestBuildMessageUsesPlainTextForPlainBodies(t *testing.T) {
	message := string(BuildMessage("a@example.com", "shop@example.com", "Hello", "plain body"))

	if !strings.Contains(message, "Content-Type: text/plain; charset=\"utf-8\"") {
		t.Fatalf("plain body was not typed as plain text: %s", message)
	}
	if !strings.Contains(message, "plain body") {
		t.Fatalf("plain body missing: %s", message)
	}
}

func TestBuildMessageStripsHeaderNewlines(t *testing.T) {
	message := string(BuildMessage("a@example.com", "shop@example.com",
		"Hello\r\nBcc: attacker@example.com", "body"))

	if strings.Contains(message, "\r\nBcc:") || strings.Contains(message, "\nBcc:") {
		t.Fatalf("header injection was not stripped: %s", message)
	}
}
