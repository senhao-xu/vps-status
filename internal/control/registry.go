package control

import "sync"

type registeredConnection struct {
	sessionID string
	close     func(string)
	sync      func()
}

// Registry owns the single active Agent connection for each node.
type Registry struct {
	mu          sync.Mutex
	connections map[string]registeredConnection
}

// NewRegistry creates an empty connection registry.
func NewRegistry() *Registry { return &Registry{connections: make(map[string]registeredConnection)} }

func (r *Registry) register(nodeID, sessionID string, close func(string), syncProbe ...func()) {
	var sync func()
	if len(syncProbe) != 0 {
		sync = syncProbe[0]
	}
	r.mu.Lock()
	previous, exists := r.connections[nodeID]
	r.connections[nodeID] = registeredConnection{sessionID: sessionID, close: close, sync: sync}
	r.mu.Unlock()
	if exists && previous.sessionID != sessionID {
		previous.close("replaced")
	}
}

// SyncProbes asks the active connection to enqueue a fresh complete snapshot.
func (r *Registry) SyncProbes(nodeID string) bool {
	r.mu.Lock()
	connection, ok := r.connections[nodeID]
	r.mu.Unlock()
	if ok && connection.sync != nil {
		connection.sync()
		return true
	}
	return false
}

func (r *Registry) remove(nodeID, sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, ok := r.connections[nodeID]; ok && current.sessionID == sessionID {
		delete(r.connections, nodeID)
	}
}

// DisconnectNode closes the current connection after credential rotation or revocation.
func (r *Registry) DisconnectNode(nodeID, reason string) bool {
	r.mu.Lock()
	connection, ok := r.connections[nodeID]
	if ok {
		delete(r.connections, nodeID)
	}
	r.mu.Unlock()
	if ok {
		connection.close(reason)
	}
	return ok
}

// DisconnectAll removes and closes every active Agent session without holding
// the registry lock while transport shutdown callbacks run.
func (r *Registry) DisconnectAll(reason string) int {
	r.mu.Lock()
	connections := make([]registeredConnection, 0, len(r.connections))
	for nodeID, connection := range r.connections {
		delete(r.connections, nodeID)
		connections = append(connections, connection)
	}
	r.mu.Unlock()
	for _, connection := range connections {
		connection.close(reason)
	}
	return len(connections)
}

// Connected reports whether a node currently owns the registry slot.
func (r *Registry) Connected(nodeID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.connections[nodeID]
	return ok
}
