package componentstate

import (
	"testing"
	"time"
)

func TestHealthyDataPlaneStaysOnlineWhenCoordinatorDegraded(t *testing.T) {
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	relay := NewSnapshot(Online, now, "public_probe_success", "公网中转正常")
	relay.LastOKAt = timePointer(now)

	overall := DeriveOverall(Components{
		Minecraft:   NewSnapshot(Ready, now, "local_port_ready", "Minecraft 正常"),
		Relay:       relay,
		Coordinator: NewSnapshot(Degraded, now, "heartbeat_failed", "Coordinator 暂时不可用"),
	}, now, 30*time.Second)

	if overall.State != Online {
		t.Fatalf("expected ONLINE, got %s (%s)", overall.State, overall.ReasonCode)
	}
}

func TestStaleRelayCannotRemainOnline(t *testing.T) {
	observed := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	now := observed.Add(31 * time.Second)
	relay := NewSnapshot(Online, observed, "public_probe_success", "公网中转正常")
	relay.LastOKAt = timePointer(observed)

	overall := DeriveOverall(Components{
		Minecraft: NewSnapshot(Ready, now, "local_port_ready", "Minecraft 正常"),
		Relay:     relay,
	}, now, 30*time.Second)

	if overall.State != Degraded || overall.ReasonCode != "relay_observation_stale" {
		t.Fatalf("expected stale DEGRADED state, got %s (%s)", overall.State, overall.ReasonCode)
	}
}

func TestStoreRetainsAtLeastFiveHundredTransitions(t *testing.T) {
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	store := NewStore(now, 2)
	for i := 0; i < 510; i++ {
		state := Connecting
		if i%2 == 0 {
			state = Offline
		}
		store.Set("relay", NewSnapshot(state, now.Add(time.Duration(i)*time.Second), "test", "test"), "op")
	}

	events := store.Events(0)
	if len(events) != 500 {
		t.Fatalf("expected 500 retained events, got %d", len(events))
	}
	if events[0].Time != now.Add(10*time.Second) {
		t.Fatalf("unexpected oldest event time %s", events[0].Time)
	}
}

func TestStoreReturnsCopies(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore(now, 500)
	components := store.Components()
	*components.Relay.ObservedAt = now.Add(time.Hour)

	got := store.Components()
	if got.Relay.ObservedAt.Equal(now.Add(time.Hour)) {
		t.Fatal("mutating a returned snapshot changed the store")
	}
}
