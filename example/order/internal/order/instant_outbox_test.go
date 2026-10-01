package order

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dirac-lee/domkit/domain"
	"github.com/dirac-lee/domkit/infra/outbox"
)

type instantTestEvent struct {
	domain.BaseDomainEvent[string]
}

func (e *instantTestEvent) EventName() string { return "instant.test" }

type instantSerializerStub struct {
	evt domain.DomainEvent
	err error
}

func (s instantSerializerStub) Serialize(_ domain.DomainEvent) ([]byte, error) {
	return nil, errors.New("serialize should not be called")
}

func (s instantSerializerStub) Deserialize(_ []byte, _ string) (domain.DomainEvent, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.evt, nil
}

type instantPublisherStub struct {
	err       error
	called    bool
	deadlined bool
}

func (p *instantPublisherStub) Publish(ctx context.Context, _ domain.DomainEvent) error {
	p.called = true
	_, p.deadlined = ctx.Deadline()
	return p.err
}

type instantStoreStub struct {
	saveErr       error
	markSentErr   error
	deadLetterErr error

	saved      *outbox.OutboxMessage
	markSentID string
	deadID     string
	deadReason string
}

func (s *instantStoreStub) SaveMessage(_ context.Context, _ any, msg *outbox.OutboxMessage) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	saved := *msg
	s.saved = &saved
	return nil
}

func (s *instantStoreStub) ClaimPending(context.Context, int, time.Duration, time.Duration, string) ([]*outbox.OutboxMessage, error) {
	return nil, nil
}

func (s *instantStoreStub) MarkSent(_ context.Context, id, token string) error {
	s.markSentID = id
	if token != instantToken {
		return errors.New("unexpected instant token")
	}
	return s.markSentErr
}

func (s *instantStoreStub) Release(context.Context, string, string, string) (int, error) {
	return 0, nil
}

func (s *instantStoreStub) MoveToDeadLetter(_ context.Context, id, token, reason string) error {
	s.deadID = id
	s.deadReason = reason
	if token != instantToken {
		return errors.New("unexpected instant token")
	}
	return s.deadLetterErr
}

func TestInstantOutboxPublishInstantMarksSent(t *testing.T) {
	store := &instantStoreStub{}
	pub := &instantPublisherStub{}
	s := newInstantOutboxStore(store, instantSerializerStub{
		evt: &instantTestEvent{BaseDomainEvent: domain.NewBaseDomainEvent("order-1", 1)},
	}, pub)

	err := s.publishInstant(outbox.OutboxMessage{ID: "msg-1", EventName: "instant.test"})
	if err != nil {
		t.Fatalf("publishInstant() unexpected error: %v", err)
	}
	if !pub.called || !pub.deadlined {
		t.Fatalf("publisher should be called with timeout context, called=%v deadlined=%v", pub.called, pub.deadlined)
	}
	if store.markSentID != "msg-1" {
		t.Fatalf("MarkSent id = %q, want msg-1", store.markSentID)
	}
}

func TestInstantOutboxPublishFailureKeepsPending(t *testing.T) {
	store := &instantStoreStub{}
	pub := &instantPublisherStub{err: errors.New("broker down")}
	s := newInstantOutboxStore(store, instantSerializerStub{
		evt: &instantTestEvent{BaseDomainEvent: domain.NewBaseDomainEvent("order-1", 1)},
	}, pub)

	err := s.publishInstant(outbox.OutboxMessage{ID: "msg-1", EventName: "instant.test"})
	if err == nil {
		t.Fatal("publishInstant() should return publisher error")
	}
	if store.markSentID != "" {
		t.Fatalf("MarkSent should not be called after publish failure, got %q", store.markSentID)
	}
	if store.deadID != "" {
		t.Fatalf("MoveToDeadLetter should not be called after publish failure, got %q", store.deadID)
	}
}

func TestInstantOutboxDeserializeFailureMovesDeadLetter(t *testing.T) {
	store := &instantStoreStub{}
	pub := &instantPublisherStub{}
	s := newInstantOutboxStore(store, instantSerializerStub{
		err: errors.New("bad payload"),
	}, pub)

	err := s.publishInstant(outbox.OutboxMessage{ID: "msg-1", EventName: "instant.test"})
	if err != nil {
		t.Fatalf("publishInstant() should swallow deserialize error after dead-letter: %v", err)
	}
	if pub.called {
		t.Fatal("publisher should not be called when payload cannot be deserialized")
	}
	if store.deadID != "msg-1" || !strings.Contains(store.deadReason, "bad payload") {
		t.Fatalf("dead-letter = (%q, %q), want msg-1 with reason", store.deadID, store.deadReason)
	}
}

func TestInstantOutboxMarkSentFailureReturnsError(t *testing.T) {
	store := &instantStoreStub{markSentErr: errors.New("claim lost")}
	pub := &instantPublisherStub{}
	s := newInstantOutboxStore(store, instantSerializerStub{
		evt: &instantTestEvent{BaseDomainEvent: domain.NewBaseDomainEvent("order-1", 1)},
	}, pub)

	err := s.publishInstant(outbox.OutboxMessage{ID: "msg-1", EventName: "instant.test"})
	if err == nil {
		t.Fatal("publishInstant() should return MarkSent error")
	}
	if !strings.Contains(err.Error(), "mark sent") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstantOutboxSaveMessageRegistersImmediatePublishWithoutTx(t *testing.T) {
	store := &instantStoreStub{}
	pub := &instantPublisherStub{}
	s := newInstantOutboxStore(store, instantSerializerStub{
		evt: &instantTestEvent{BaseDomainEvent: domain.NewBaseDomainEvent("order-1", 1)},
	}, pub)

	err := s.SaveMessage(context.Background(), nil, &outbox.OutboxMessage{ID: "msg-1", EventName: "instant.test"})
	if err != nil {
		t.Fatalf("SaveMessage() unexpected error: %v", err)
	}
	if store.saved == nil || store.saved.ID != "msg-1" {
		t.Fatalf("inner SaveMessage should receive message, got %+v", store.saved)
	}
	if store.markSentID != "msg-1" {
		t.Fatalf("post-commit publish should mark sent immediately without tx, got %q", store.markSentID)
	}
}
