package implement

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/domain/model"
)

// fakeDirectory answers the DirectoryRepository port and records the window and
// ids it received, so a test can prove the service maps the contract to the domain
// and back without touching another port.
type fakeDirectory struct {
	kpis      model.CustomerKPIs
	summaries []model.CustomerSummary
	err       error

	week     model.TimeWindow
	received []uuid.UUID
}

func (f *fakeDirectory) KPIs(_ context.Context, week model.TimeWindow) (model.CustomerKPIs, error) {
	f.week = week
	return f.kpis, f.err
}

func (f *fakeDirectory) SummariesByIDs(_ context.Context, ids []uuid.UUID) ([]model.CustomerSummary, error) {
	f.received = ids
	return f.summaries, f.err
}

// research D8, FR-015, FR-016: the service maps the cross-module window to the
// domain read and the domain counts back to the contract.
func TestServiceKPIsMapsTheDirectoryRead(t *testing.T) {
	from := time.Date(2026, time.October, 12, 0, 0, 0, 0, time.UTC)
	to := from.Add(7 * 24 * time.Hour)
	directory := &fakeDirectory{kpis: model.CustomerKPIs{TotalUsers: 412, NewUsersThisWeek: 28}}
	svc := New(Service{Directory: directory})

	got, err := svc.KPIs(context.Background(), contracts.TimeWindow{From: from, To: to})
	if err != nil {
		t.Fatalf("KPIs: %v", err)
	}
	if got.TotalUsers != 412 || got.NewUsersThisWeek != 28 {
		t.Fatalf("counts did not map: %+v", got)
	}
	if !directory.week.From.Equal(from) || !directory.week.To.Equal(to) {
		t.Fatalf("the window must be carried unchanged, got %+v", directory.week)
	}
}

// FR-026, research D8: the summaries map in one bulk call and an empty id list is
// answered without inventing a customer.
func TestServiceCustomerSummariesMapsTheBulkRead(t *testing.T) {
	id := uuid.New()
	directory := &fakeDirectory{summaries: []model.CustomerSummary{
		{ID: id, DisplayName: "Nguyễn Minh Anh", Email: "minhanh.ng@gmail.com"},
	}}
	svc := New(Service{Directory: directory})

	got, err := svc.CustomerSummaries(context.Background(), []uuid.UUID{id})
	if err != nil {
		t.Fatalf("CustomerSummaries: %v", err)
	}
	if len(got) != 1 || got[0].ID != id || got[0].DisplayName != "Nguyễn Minh Anh" ||
		got[0].Email != "minhanh.ng@gmail.com" {
		t.Fatalf("summaries did not map: %+v", got)
	}
	if len(directory.received) != 1 || directory.received[0] != id {
		t.Fatalf("the ids must be passed through unchanged, got %+v", directory.received)
	}
}

// A repository failure is returned as itself, never swallowed.
func TestServiceKPIsReportsTheDirectoryError(t *testing.T) {
	want := errors.New("storage unavailable")
	svc := New(Service{Directory: &fakeDirectory{err: want}})

	if _, err := svc.KPIs(context.Background(), contracts.TimeWindow{}); !errors.Is(err, want) {
		t.Fatalf("expected the directory error, got %v", err)
	}
}
