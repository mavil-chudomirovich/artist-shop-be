package model

import (
	"errors"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
)

func TestNormalizeEmail(t *testing.T) {
	if got := NormalizeEmail("  User@Example.COM "); got != "user@example.com" {
		t.Fatalf("unexpected normalized email %q", got)
	}
}

func TestAccountTransitions(t *testing.T) {
	cases := []struct {
		name    string
		from    constant.Status
		call    func(*Account) error
		want    constant.Status
		wantErr error
	}{
		{name: "activate pending", from: constant.StatusPending, call: (*Account).Activate, want: constant.StatusActive},
		{name: "activate active rejected", from: constant.StatusActive, call: (*Account).Activate, want: constant.StatusActive, wantErr: domainerr.ErrInvalidTransition},
		{name: "activate disabled rejected", from: constant.StatusDisabled, call: (*Account).Activate, want: constant.StatusDisabled, wantErr: domainerr.ErrInvalidTransition},
		{name: "disable active", from: constant.StatusActive, call: (*Account).Disable, want: constant.StatusDisabled},
		{name: "disable pending rejected", from: constant.StatusPending, call: (*Account).Disable, want: constant.StatusPending, wantErr: domainerr.ErrInvalidTransition},
		{name: "enable disabled", from: constant.StatusDisabled, call: (*Account).Enable, want: constant.StatusActive},
		{name: "enable active rejected", from: constant.StatusActive, call: (*Account).Enable, want: constant.StatusActive, wantErr: domainerr.ErrInvalidTransition},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{Status: tc.from}
			err := tc.call(account)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
			if account.Status != tc.want {
				t.Fatalf("expected status %s, got %s", tc.want, account.Status)
			}
		})
	}
}

func TestCanSignIn(t *testing.T) {
	cases := []struct {
		status  constant.Status
		wantErr error
	}{
		{status: constant.StatusActive},
		{status: constant.StatusPending, wantErr: domainerr.ErrAccountPending},
		{status: constant.StatusDisabled, wantErr: domainerr.ErrAccountDisabled},
		{status: constant.Status("archived"), wantErr: domainerr.ErrInvalidTransition},
	}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			account := &Account{Status: tc.status}
			if err := account.CanSignIn(); !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}
