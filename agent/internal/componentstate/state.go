package componentstate

import (
	"sync"
	"time"
)

type State string

const (
	Offline      State = "OFFLINE"
	Connecting   State = "CONNECTING"
	Online       State = "ONLINE"
	Reconnecting State = "RECONNECTING"
	Degraded     State = "DEGRADED"
	Stopping     State = "STOPPING"
	Error        State = "ERROR"

	Stopped  State = "STOPPED"
	Starting State = "STARTING"
	Ready    State = "READY"

	Unknown      State = "UNKNOWN"
	AuthFailed   State = "AUTH_FAILED"
	Incompatible State = "INCOMPATIBLE"
)

type Snapshot struct {
	State            State      `json:"state"`
	UpdatedAt        time.Time  `json:"updated_at"`
	ObservedAt       *time.Time `json:"observed_at,omitempty"`
	LastOKAt         *time.Time `json:"last_ok_at,omitempty"`
	ReasonCode       string     `json:"reason_code,omitempty"`
	UserMessage      string     `json:"user_message"`
	TechnicalMessage string     `json:"technical_message,omitempty"`
}

type Components struct {
	Minecraft   Snapshot `json:"minecraft"`
	Relay       Snapshot `json:"relay"`
	Coordinator Snapshot `json:"coordinator"`
}

type Event struct {
	Event       string    `json:"event"`
	Component   string    `json:"component"`
	From        State     `json:"from"`
	To          State     `json:"to"`
	Reason      string    `json:"reason"`
	OperationID string    `json:"operation_id,omitempty"`
	Time        time.Time `json:"time"`
}

func NewSnapshot(state State, now time.Time, reason, message string) Snapshot {
	now = now.UTC()
	return Snapshot{
		State:       state,
		UpdatedAt:   now,
		ObservedAt:  timePointer(now),
		ReasonCode:  reason,
		UserMessage: message,
	}
}

func (s Snapshot) Fresh(now time.Time, ttl time.Duration) bool {
	if ttl <= 0 {
		return false
	}
	observed := s.ObservedAt
	if s.LastOKAt != nil {
		observed = s.LastOKAt
	}
	if observed == nil || observed.IsZero() {
		return false
	}
	age := now.Sub(*observed)
	return age >= 0 && age <= ttl
}

func DeriveOverall(components Components, now time.Time, relayTTL time.Duration) Snapshot {
	state := Offline
	reason := "components_offline"
	message := "当前没有托管服务器"

	switch {
	case components.Minecraft.State == Error || components.Relay.State == Error:
		state, reason, message = Error, "data_plane_error", "公网服务发生错误"
	case components.Minecraft.State == Stopping || components.Relay.State == Stopping:
		state, reason, message = Stopping, "stop_in_progress", "正在停止托管"
	case components.Minecraft.State == Ready && components.Relay.State == Online && components.Relay.Fresh(now, relayTTL):
		state, reason, message = Online, "data_plane_healthy", "公网服务器运行正常"
	case components.Minecraft.State == Ready && components.Relay.State == Online:
		state, reason, message = Degraded, "relay_observation_stale", "公网中转状态已过期"
	case components.Relay.State == Reconnecting:
		state, reason, message = Reconnecting, "relay_reconnecting", "正在重新连接公网中转"
	case components.Minecraft.State == Starting || components.Relay.State == Connecting || components.Coordinator.State == Connecting:
		state, reason, message = Connecting, "start_in_progress", "正在启动公网服务"
	case components.Minecraft.State == Ready:
		state, reason, message = Degraded, "relay_unavailable", "本地服务器正常，公网中转不可用"
	}

	return NewSnapshot(state, now, reason, message)
}

type Store struct {
	mu         sync.RWMutex
	components Components
	events     []Event
	capacity   int
}

func NewStore(now time.Time, capacity int) *Store {
	if capacity < 500 {
		capacity = 500
	}
	return &Store{
		components: Components{
			Minecraft:   NewSnapshot(Unknown, now, "not_checked", "尚未探测本地服务器"),
			Relay:       NewSnapshot(Offline, now, "not_started", "公网中转未启动"),
			Coordinator: NewSnapshot(Unknown, now, "not_checked", "尚未连接 Coordinator"),
		},
		capacity: capacity,
		events:   make([]Event, 0, capacity),
	}
}

func (s *Store) Components() Components {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneComponents(s.components)
}

func (s *Store) Set(component string, next Snapshot, operationID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.component(component)
	if current == nil {
		return false
	}
	previous := current.State
	*current = cloneSnapshot(next)
	if previous == next.State {
		return true
	}

	event := Event{
		Event:       "state_transition",
		Component:   component,
		From:        previous,
		To:          next.State,
		Reason:      next.ReasonCode,
		OperationID: operationID,
		Time:        next.UpdatedAt.UTC(),
	}
	if len(s.events) == s.capacity {
		copy(s.events, s.events[1:])
		s.events[len(s.events)-1] = event
	} else {
		s.events = append(s.events, event)
	}
	return true
}

func (s *Store) Events(limit int) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > len(s.events) {
		limit = len(s.events)
	}
	start := len(s.events) - limit
	result := make([]Event, limit)
	copy(result, s.events[start:])
	return result
}

func (s *Store) component(name string) *Snapshot {
	switch name {
	case "minecraft", "local_probe":
		return &s.components.Minecraft
	case "relay":
		return &s.components.Relay
	case "coordinator":
		return &s.components.Coordinator
	default:
		return nil
	}
}

func cloneComponents(value Components) Components {
	value.Minecraft = cloneSnapshot(value.Minecraft)
	value.Relay = cloneSnapshot(value.Relay)
	value.Coordinator = cloneSnapshot(value.Coordinator)
	return value
}

func cloneSnapshot(value Snapshot) Snapshot {
	if value.ObservedAt != nil {
		observed := *value.ObservedAt
		value.ObservedAt = &observed
	}
	if value.LastOKAt != nil {
		lastOK := *value.LastOKAt
		value.LastOKAt = &lastOK
	}
	return value
}

func timePointer(value time.Time) *time.Time {
	return &value
}
