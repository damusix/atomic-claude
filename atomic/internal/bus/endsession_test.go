package bus

import (
	"testing"
)

func TestEndSession_ClosesOnlyThatMembersStream(t *testing.T) {
	h := NewHub(t.TempDir())

	victim, err := h.Join("room", "victim", "", KindAgent, "sess-victim", "", "")
	if err != nil {
		t.Fatalf("join victim: %v", err)
	}
	bystander, err := h.Join("room", "bystander", "", KindAgent, "sess-bystander", "", "")
	if err != nil {
		t.Fatalf("join bystander: %v", err)
	}

	victimCh := make(chan Envelope, 4)
	bystanderCh := make(chan Envelope, 4)
	defer h.Subscribe("room", victimCh, "sess-victim", false)()
	defer h.Subscribe("room", bystanderCh, "sess-bystander", false)()

	if _, err := h.EndSession("room", victim); err != nil {
		t.Fatalf("end session: %v", err)
	}

	// recvDeliver reconnects on a bare close, so the envelope must say why.
	env, ok := <-victimCh
	if !ok {
		t.Fatal("victim stream closed with no envelope; recv would treat that as a daemon restart and reconnect")
	}
	if !env.Closing {
		t.Errorf("envelope Closing = false; recv reconnects unless the last envelope is marked closing")
	}
	if _, stillOpen := <-victimCh; stillOpen {
		t.Error("victim stream stayed open after the closing envelope")
	}

	// Closing reads as "last envelope on this stream", so a peer must not see it.
	select {
	case env, ok := <-bystanderCh:
		if !ok {
			t.Fatal("bystander stream was closed; ending one session must not end the room")
		}
		t.Errorf("bystander received %+v; a closing envelope for someone else primes it to stop reconnecting", env)
	default:
	}

	members, err := h.Who("room")
	if err != nil {
		t.Fatalf("who: %v", err)
	}
	if len(members) != 1 || members[0].Name != bystander {
		t.Errorf("roster = %+v, want only %s", members, bystander)
	}
}

func TestEndSession_UnknownMember(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("room", "present", "", KindAgent, "sess-present", "", ""); err != nil {
		t.Fatalf("join: %v", err)
	}
	if _, err := h.EndSession("room", "absent"); err == nil {
		t.Error("ending a member that is not in the room reported success")
	}
}

func TestEndSession_UnknownRoom(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.EndSession("nope", "someone"); err == nil {
		t.Error("ending a session in a room that does not exist reported success")
	}
}
