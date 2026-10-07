//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/model"
)

// FR-020, FR-021 against real PostgreSQL: several operators create the same name
// at the same instant. The storage unique index is what makes exactly one win;
// the adapter's classification is what makes the losers answer 409 rather than
// 500. Both are asserted, because a duplicate-key violation surfacing as an
// internal error is a fail even though no duplicate was stored.
//
// The requests are fired from separate goroutines released together by a closed
// channel, so they genuinely overlap. A sequential run would pass this test while
// proving nothing about the race.
func TestConcurrentCreationsOfTheSameNameProduceOneWinner(t *testing.T) {
	f := newMaintenanceFixture(t)

	const contenders = 8
	body := `{"name":"Tranh sơn dầu","slug":"tranh-son-dau","position":1}`

	type answer struct {
		status int
		code   string
	}
	answers := make([]answer, contenders)

	// A start barrier: every goroutine blocks until the channel closes, so all
	// contenders enter the handler as close to together as the scheduler allows.
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			rec := f.call(http.MethodPost, adminCategoriesPath, body, f.adminToken)
			answers[i].status = rec.Code
			// Decode without t: FailNow must never be called from a goroutine
			// other than the one running the test.
			var envelope struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &envelope)
			answers[i].code = envelope.Error.Code
		}(i)
	}
	close(start)
	wg.Wait()
	t.Logf("concurrent create answers: %+v", answers)

	created, conflicts := 0, 0
	for i, got := range answers {
		switch got.status {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicts++
			if got.code != "CATEGORY_NAME_TAKEN" {
				t.Errorf("contender %d lost but answered code %q, want CATEGORY_NAME_TAKEN", i, got.code)
			}
		case http.StatusInternalServerError:
			t.Errorf("contender %d surfaced a duplicate-key violation as an internal error, not a conflict", i)
		default:
			t.Errorf("contender %d answered an unexpected status %d (code %q)", i, got.status, got.code)
		}
	}
	if created != 1 {
		t.Fatalf("exactly one creation must win, got %d (answers: %+v)", created, answers)
	}
	if conflicts != contenders-1 {
		t.Fatalf("every loser must be told the name collided: expected %d conflicts, got %d (answers: %+v)",
			contenders-1, conflicts, answers)
	}

	// The storage index is what makes the count one: an application-level check
	// would let two concurrent creates both pass and both insert.
	var stored int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM categories WHERE normalized_name = $1`,
		model.FoldKey("Tranh sơn dầu")).Scan(&stored); err != nil {
		t.Fatalf("count the stored categories: %v", err)
	}
	if stored != 1 {
		t.Fatalf("exactly one row must be stored, got %d", stored)
	}

	// Only the winner is audited as a success, so no loser was reported as a
	// write that happened.
	f.waitForAuditRows(t, constant.AuditCategoryCreated, 1)
}
