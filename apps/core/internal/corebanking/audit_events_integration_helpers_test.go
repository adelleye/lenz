//go:build integration

package corebanking

import "testing"

func assertAuditActorContext(t *testing.T, events []AuditEvent, actorType, actorID, requestID string, metadata map[string]string) {
	t.Helper()
	for _, event := range events {
		if event.ActorType != actorType || event.ActorID != actorID || event.RequestID != requestID {
			t.Fatalf("audit event missing actor/request context: %+v", event)
		}
		for key, want := range metadata {
			if got := event.Metadata[key]; got != want {
				t.Fatalf("audit event metadata %s=%q, want %q in %+v", key, got, want, event)
			}
		}
	}
}
