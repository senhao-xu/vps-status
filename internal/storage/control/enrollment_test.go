package control

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/security"
)

func TestEnrollmentRetentionRevocationAndExpiry(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	makeToken := func(id string, at time.Time) EnrollmentToken {
		return EnrollmentToken{ID: id, Digest: security.DigestEnrollmentToken("secret-" + id), NodeName: "edge", CreatedAt: at, ExpiresAt: at.Add(time.Hour)}
	}
	first := makeToken("first", now)
	if err := store.CreateEnrollmentToken(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	status, err := store.EnrollmentByID(t.Context(), first.ID)
	if err != nil || status.ID != first.ID || status.NodeName != "edge" || status.ConsumedNodeID != "" || !status.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("pending=%+v err=%v", status, err)
	}
	if _, err := store.ConsumeEnrollment(t.Context(), first.Digest, "node-first", "cred-first", security.DigestNodeCredential("credential"), now); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateEnrollmentToken(t.Context(), makeToken("second", now)); err != nil {
		t.Fatal(err)
	}
	retained, err := store.EnrollmentByID(t.Context(), first.ID)
	if err != nil || retained.ConsumedNodeID != "node-first" {
		t.Fatalf("consumed record lost during creation: %+v err=%v", retained, err)
	}
	if err := store.RevokeEnrollment(t.Context(), first.ID); !errors.Is(err, ErrEnrollmentConsumed) {
		t.Fatalf("consumed revocation err=%v", err)
	}
	if err := store.RevokeEnrollment(t.Context(), "second"); err != nil {
		t.Fatal(err)
	}
	second := makeToken("second", now)
	if _, err := store.ConsumeEnrollment(t.Context(), second.Digest, "node-second", "cred-second", security.DigestNodeCredential("other"), now); !errors.Is(err, ErrEnrollmentInvalid) {
		t.Fatalf("revoked token consumption err=%v", err)
	}
	if err := store.RevokeEnrollment(t.Context(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing revocation err=%v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := store.RevokeEnrollment(ctx, first.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled revocation err=%v", err)
	}
	if err := store.CreateEnrollmentToken(t.Context(), makeToken("third", first.ExpiresAt)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnrollmentByID(t.Context(), first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired record was not pruned: %v", err)
	}
	if _, err := store.NodeByID(t.Context(), "node-first"); err != nil {
		t.Fatalf("pruning removed registered node: %v", err)
	}
}

func TestEnrollmentConcurrentConsumptionAndRevocation(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	for attempt := 0; attempt < 24; attempt++ {
		id := fmt.Sprintf("race-%d", attempt)
		token := EnrollmentToken{ID: id, Digest: security.DigestEnrollmentToken(id), NodeName: "edge", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
		if err := store.CreateEnrollmentToken(t.Context(), token); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		consumeResult, revokeResult := make(chan error, 1), make(chan error, 1)
		go func() {
			<-start
			_, err := store.ConsumeEnrollment(t.Context(), token.Digest, "node-"+id, "cred-"+id, security.DigestNodeCredential(id), now)
			consumeResult <- err
		}()
		go func() {
			<-start
			revokeResult <- store.RevokeEnrollment(t.Context(), id)
		}()
		close(start)
		consumeErr, revokeErr := <-consumeResult, <-revokeResult
		switch {
		case consumeErr == nil && errors.Is(revokeErr, ErrEnrollmentConsumed):
			retained, err := store.EnrollmentByID(t.Context(), id)
			if err != nil || retained.ConsumedNodeID != "node-"+id {
				t.Fatalf("consumption winner lost node association: %+v %v", retained, err)
			}
		case revokeErr == nil && errors.Is(consumeErr, ErrEnrollmentInvalid):
			if _, err := store.NodeByID(t.Context(), "node-"+id); !errors.Is(err, ErrNotFound) {
				t.Fatalf("revocation winner left a node: %v", err)
			}
		default:
			t.Fatalf("invalid race outcome: consume=%v revoke=%v", consumeErr, revokeErr)
		}
	}
}
