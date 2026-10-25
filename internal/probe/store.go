package probe

import (
	"slices"
	"sync"
)

// Store holds the last poll of every service, so a page load reads state instead of triggering
// probes (decision #11). Its zero value is ready to use.
type Store struct {
	mu     sync.RWMutex
	states map[string]State
}

// Update records a service's newest state. The store keeps its own copy, so a caller may go on using
// the state it passed in.
func (store *Store) Update(id string, state State) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.states == nil {
		store.states = make(map[string]State)
	}
	store.states[id] = cloneState(state)
}

// Snapshot returns every service's state as a copy that is safe to hold and to modify.
func (store *Store) Snapshot() map[string]State {
	store.mu.RLock()
	defer store.mu.RUnlock()
	snapshot := make(map[string]State, len(store.states))
	for id, state := range store.states {
		snapshot[id] = cloneState(state)
	}
	return snapshot
}

// state returns one service's last result, as a copy. It is the zero state when the service has
// never been polled.
func (store *Store) state(id string) (State, bool) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	state, found := store.states[id]
	return cloneState(state), found
}

// Retain drops every service whose id is not in ids, which is how a reload forgets removed services.
func (store *Store) Retain(ids []string) {
	keep := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		keep[id] = struct{}{}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	for id := range store.states {
		if _, kept := keep[id]; !kept {
			delete(store.states, id)
		}
	}
}

// cloneState copies the slice a State carries, so no two owners share it.
func cloneState(state State) State {
	state.Vitals = slices.Clone(state.Vitals)
	return state
}
