package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/sse"
)

// This file is the transport contract of the live streams
// (specs/020-order-sse-stream): the customer stream needs a session, the admin
// stream needs ADMIN, both speak SSE, and a handler without a hub answers 503
// instead of hanging the connection.

func newStreamRouter(svc Service, hub *sse.Hub) http.Handler {
	root := chi.NewRouter()
	root.Mount(ordersPath, New(svc, testLogger).SetStreams(hub).Router(orderHooks()))
	root.Mount(adminOrdersPath, New(svc, testLogger).SetStreams(hub).AdminRouter(orderHooks()))
	return root
}

func TestCustomerStreamNeedsASession(t *testing.T) {
	router := newStreamRouter(&fakeOrders{}, sse.New(nil))
	rec := perform(router, http.MethodGet, ordersPath+"/stream", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("stream without a session must answer 401, got %d", rec.Code)
	}
}

func TestAdminStreamNeedsAnAdministrator(t *testing.T) {
	router := newStreamRouter(&fakeOrders{}, sse.New(nil))

	rec := perform(router, http.MethodGet, adminOrdersPath+"/stream", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("admin stream without a session must answer 401, got %d", rec.Code)
	}

	rec = perform(router, http.MethodGet, adminOrdersPath+"/stream", "", "customer-token")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a customer on the admin stream must answer 403, got %d", rec.Code)
	}
}

func TestStreamsWithoutAHubAnswerUnavailable(t *testing.T) {
	root := chi.NewRouter()
	root.Mount(ordersPath, New(&fakeOrders{}, testLogger).Router(orderHooks()))
	root.Mount(adminOrdersPath, New(&fakeOrders{}, testLogger).AdminRouter(orderHooks()))

	if rec := perform(root, http.MethodGet, ordersPath+"/stream", "", "customer-token"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("a hub-less customer stream must answer 503, got %d", rec.Code)
	}
	if rec := perform(root, http.MethodGet, adminOrdersPath+"/stream", "", "admin-token"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("a hub-less admin stream must answer 503, got %d", rec.Code)
	}
}

func TestCustomerStreamDeliversItsOwnEvent(t *testing.T) {
	hub := sse.New(nil)
	handler := New(&fakeOrders{}, testLogger).SetStreams(hub)

	ctx, cancel := context.WithCancel(context.Background())
	// The router's RequireAuthentication resolves the session from the
	// Authorization header the fake hooks read.
	router := chi.NewRouter()
	router.Mount(ordersPath, handler.Router(orderHooks()))

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Serve through the mounted router in a goroutine: the stream blocks
		// until the client goes away.
		streamReq := httptest.NewRequest(http.MethodGet, ordersPath+"/stream", nil)
		streamReq.Header.Set("Authorization", "Bearer customer-token")
		streamReq = streamReq.WithContext(ctx)
		router.ServeHTTP(rec, streamReq)
	}()

	// Wait for the subscription, publish to the customer's topic and to
	// another customer's, then close the stream.
	deadline := time.Now().Add(2 * time.Second)
	for hub.SubscriberCount("user:"+testCustomerID.String()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	hub.Publish("user:"+testCustomerID.String(), "order.status_changed", []byte(`{"orderId":"1"}`))
	hub.Publish("user:00000000-0000-0000-0000-000000000000", "order.status_changed", []byte(`{"orderId":"other"}`))
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not return after client cancel")
	}

	body := rec.Body.String()
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if !strings.Contains(body, "event: order.status_changed") || !strings.Contains(body, `{"orderId":"1"}`) {
		t.Fatalf("stream must carry the customer's event, got %q", body)
	}
	if strings.Contains(body, `"other"`) {
		t.Fatalf("stream must not carry another customer's event, got %q", body)
	}
	if got := hub.SubscriberCount("user:" + testCustomerID.String()); got != 0 {
		t.Fatalf("subscriber leaked after disconnect: %d", got)
	}
}
