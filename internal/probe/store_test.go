package probe_test

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/probe"
)

// polledState is a state with one vital, so a store test can check that the slice inside it is
// copied as well as the map around it.
func polledState(value string) probe.State {
	return probe.State{
		Health:    probe.Health{Status: probe.StatusOK},
		CheckedAt: polledAt,
		Vitals:    []probe.VitalState{{Label: "needs you", Value: value, At: polledAt}},
	}
}

func TestStore_UpdateAndSnapshot(t *testing.T) {
	var store probe.Store

	store.Update("ivy", polledState("4"))
	store.Update("adguard", polledState("30"))

	snapshot := store.Snapshot()
	if len(snapshot) != 2 {
		t.Fatalf("snapshot has %d services, want 2", len(snapshot))
	}
	assertState(t, snapshot["ivy"], polledState("4"))
	assertState(t, snapshot["adguard"], polledState("30"))
}

func TestStore_SnapshotIsACopy(t *testing.T) {
	var store probe.Store
	store.Update("ivy", polledState("4"))

	snapshot := store.Snapshot()
	snapshot["ivy"] = polledState("99")
	snapshot["new"] = polledState("1")
	delete(snapshot, "ivy")
	held := store.Snapshot()["ivy"]
	held.Vitals[0].Value = "tampered"

	again := store.Snapshot()
	if _, found := again["new"]; found {
		t.Fatalf("a snapshot write reached the store: %+v", again)
	}
	assertState(t, again["ivy"], polledState("4"))
}

func TestStore_RetainDropsRemovedServices(t *testing.T) {
	var store probe.Store
	store.Update("ivy", polledState("4"))
	store.Update("adguard", polledState("30"))
	store.Update("bwmon", polledState("2"))

	store.Retain([]string{"ivy", "bwmon"})

	snapshot := store.Snapshot()
	names := make([]string, 0, len(snapshot))
	for id := range snapshot {
		names = append(names, id)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"bwmon", "ivy"}) {
		t.Fatalf("kept %v, want [bwmon ivy]", names)
	}
}

func TestStore_HandlesPollsAndReadsTogether(t *testing.T) {
	var store probe.Store
	var waitGroup sync.WaitGroup

	for worker := range 8 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for round := range 50 {
				id := []string{"ivy", "adguard", "bwmon"}[round%3]
				store.Update(id, polledState("4"))
				snapshot := store.Snapshot()
				for _, state := range snapshot {
					if derived := probe.Derive(state, true); derived != probe.StatusOK {
						t.Errorf("Derive() = %q, want %q", derived, probe.StatusOK)
					}
				}
				if worker%2 == 0 {
					store.Retain([]string{"ivy", "adguard"})
				}
			}
		}()
	}

	waitGroup.Wait()
	_ = store.Snapshot()
}

func TestStore_UpdateReplacesTheWholeState(t *testing.T) {
	var store probe.Store
	store.Update("ivy", polledState("4"))
	store.Update("ivy", probe.State{Health: probe.Health{Status: probe.StatusDown}, CheckedAt: polledAt.Add(time.Minute)})

	snapshot := store.Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("snapshot has %d services, want 1", len(snapshot))
	}
	if snapshot["ivy"].Health.Status != probe.StatusDown || len(snapshot["ivy"].Vitals) != 0 {
		t.Fatalf("state = %+v, want the newer one", snapshot["ivy"])
	}
}
