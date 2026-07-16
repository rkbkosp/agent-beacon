package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	Version         = 2
	MaxMessageBytes = 64 * 1024
)

type MessageType string

const (
	TypeHello        MessageType = "hello"
	TypeSnapshot     MessageType = "snapshot"
	TypeStatePatch   MessageType = "state_patch"
	TypeNotification MessageType = "notification"
	TypeHeartbeat    MessageType = "heartbeat"
	TypeError        MessageType = "error"
	TypeGetSnapshot  MessageType = "get_snapshot"
	TypeACK          MessageType = "ack"
	TypeDeviceStatus MessageType = "device_status"
	TypeButtonAction MessageType = "button_action"
)

type Theme string

const (
	ThemeBlue   Theme = "blue"
	ThemeYellow Theme = "yellow"
	ThemeRed    Theme = "red"
	ThemeGreen  Theme = "green"
)

type Category string

const (
	CategoryAgent   Category = "agent"
	CategoryQuota   Category = "quota"
	CategoryWeather Category = "weather"
	CategorySystem  Category = "system"
)

type Urgency string

const (
	UrgencyNormal    Urgency = "normal"
	UrgencyAttention Urgency = "attention"
	UrgencyUrgent    Urgency = "urgent"
)

type ACKStatus string

const (
	ACKReceived    ACKStatus = "received"
	ACKQueued      ACKStatus = "queued"
	ACKShown       ACKStatus = "shown"
	ACKCompleted   ACKStatus = "completed"
	ACKInterrupted ACKStatus = "interrupted"
	ACKSuperseded  ACKStatus = "superseded"
	ACKExpired     ACKStatus = "expired"
	ACKDropped     ACKStatus = "dropped"
	ACKInvalid     ACKStatus = "invalid"
	ACKDuplicate   ACKStatus = "duplicate"
)

type Freshness string

const (
	FreshnessFresh   Freshness = "fresh"
	FreshnessCached  Freshness = "cached"
	FreshnessStale   Freshness = "stale"
	FreshnessUnknown Freshness = "unknown"
)

type AgentStatus string

const (
	AgentWorking AgentStatus = "working"
	AgentBlocked AgentStatus = "blocked"
	AgentDone    AgentStatus = "done"
	AgentIdle    AgentStatus = "idle"
	AgentUnknown AgentStatus = "unknown"
)

type Envelope struct {
	V        int             `json:"v"`
	ID       string          `json:"id"`
	Type     MessageType     `json:"type"`
	TS       time.Time       `json:"ts"`
	Revision uint64          `json:"revision"`
	Payload  json.RawMessage `json:"payload"`
}

type Notification struct {
	Category             Category  `json:"category"`
	Kind                 string    `json:"kind"`
	Source               string    `json:"source"`
	SubjectID            string    `json:"subject_id"`
	Theme                Theme     `json:"theme"`
	Urgency              Urgency   `json:"urgency"`
	Priority             uint8     `json:"priority"`
	DedupeKey            string    `json:"dedupe_key"`
	SupersedeKey         string    `json:"supersede_key,omitempty"`
	GroupKey             string    `json:"group_key,omitempty"`
	Title                string    `json:"title"`
	Detail               string    `json:"detail,omitempty"`
	SourceLabel          string    `json:"source_label,omitempty"`
	DisplayMS            uint32    `json:"display_ms"`
	ExpiresAt            time.Time `json:"expires_at"`
	StickyBadge          bool      `json:"sticky_badge"`
	ReplayAfterInterrupt bool      `json:"replay_after_interrupt"`
	MaxReplays           uint8     `json:"max_replays"`
}

type ACK struct {
	V        int         `json:"v"`
	Type     MessageType `json:"type"`
	ID       string      `json:"id"`
	DeviceID string      `json:"device_id"`
	Status   ACKStatus   `json:"status"`
	At       time.Time   `json:"at"`
	Reason   string      `json:"reason,omitempty"`
}

type Hello struct {
	Role            string `json:"role"`
	DeviceID        string `json:"device_id,omitempty"`
	ProtocolVersion int    `json:"protocol_version"`
	FirmwareVersion string `json:"firmware_version,omitempty"`
	BridgeVersion   string `json:"bridge_version,omitempty"`
}

type GetSnapshot struct {
	Reason string `json:"reason,omitempty"`
}

type Heartbeat struct {
	DeviceID string `json:"device_id,omitempty"`
}

type ClockState struct {
	Timezone   string    `json:"timezone"`
	ServerTime time.Time `json:"server_time"`
}

type CodexHome struct {
	ID                        string     `json:"id"`
	Label                     string     `json:"label"`
	WeeklyRemainingPercent    int        `json:"weekly_remaining_percent"`
	WeeklyResetAt             *time.Time `json:"weekly_reset_at"`
	ResetCardsAvailable       *int       `json:"reset_cards_available"`
	NearestResetCardExpiresAt *time.Time `json:"nearest_reset_card_expires_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
	Freshness                 Freshness  `json:"freshness"`
}

type RelayState struct {
	Remaining *float64  `json:"remaining"`
	Unit      string    `json:"unit"`
	IsValid   bool      `json:"is_valid"`
	UpdatedAt time.Time `json:"updated_at"`
	Freshness Freshness `json:"freshness"`
}

type TokenRateState struct {
	TokensPerSecond *float64   `json:"tokens_per_second"`
	ActiveSessions  int        `json:"active_sessions"`
	ActiveStreams   int        `json:"active_streams"`
	WindowMS        uint32     `json:"window_ms"`
	Estimated       bool       `json:"estimated"`
	UpdatedAt       *time.Time `json:"updated_at"`
	Freshness       Freshness  `json:"freshness"`
}

type CodexState struct {
	Homes     []CodexHome    `json:"homes"`
	Relay     RelayState     `json:"relay"`
	TokenRate TokenRateState `json:"token_rate"`
}

type AgentItem struct {
	PaneID       string        `json:"pane_id"`
	TerminalID   string        `json:"terminal_id,omitempty"`
	WorkspaceID  string        `json:"workspace_id,omitempty"`
	TabID        string        `json:"tab_id,omitempty"`
	Agent        string        `json:"agent,omitempty"`
	DisplayName  string        `json:"display_name"`
	Status       AgentStatus   `json:"status"`
	CustomStatus string        `json:"custom_status,omitempty"`
	Title        string        `json:"title,omitempty"`
	Focused      bool          `json:"focused"`
	Revision     uint64        `json:"revision"`
	AgentSession *AgentSession `json:"agent_session,omitempty"`
}

type AgentSession struct {
	Source string `json:"source"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

type AgentsState struct {
	Provider    string      `json:"provider"`
	Connected   bool        `json:"connected"`
	CodexActive bool        `json:"codex_active"`
	UpdatedAt   time.Time   `json:"updated_at"`
	Items       []AgentItem `json:"items"`
}

type WeatherCurrent struct {
	ObservedAt time.Time `json:"observed_at"`
	TempC      int       `json:"temp_c"`
	Icon       string    `json:"icon"`
	Text       string    `json:"text"`
	PrecipMM   float64   `json:"precip_mm"`
	Freshness  Freshness `json:"freshness"`
}

type WeatherSlot struct {
	TargetAt  time.Time `json:"target_at"`
	IsPast    bool      `json:"is_past"`
	TempC     int       `json:"temp_c"`
	Icon      string    `json:"icon"`
	Text      string    `json:"text"`
	POP       int       `json:"pop"`
	PrecipMM  float64   `json:"precip_mm"`
	Freshness Freshness `json:"freshness"`
}

type NextOuting struct {
	Slot             string    `json:"slot"`
	TargetAt         time.Time `json:"target_at"`
	UmbrellaRequired *bool     `json:"umbrella_required"`
	Confidence       string    `json:"confidence"`
	Reason           string    `json:"reason"`
}

type WeatherState struct {
	Location   string         `json:"location"`
	Provider   string         `json:"provider"`
	Current    WeatherCurrent `json:"current"`
	Lunch      WeatherSlot    `json:"lunch"`
	Leave      WeatherSlot    `json:"leave"`
	NextOuting NextOuting     `json:"next_outing"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

type SystemState struct {
	BridgeOnline     bool      `json:"bridge_online"`
	OverallFreshness Freshness `json:"overall_freshness"`
}

type Snapshot struct {
	Clock   ClockState   `json:"clock"`
	Codex   CodexState   `json:"codex"`
	Agents  AgentsState  `json:"agents"`
	Weather WeatherState `json:"weather"`
	System  SystemState  `json:"system"`
}

type StatePatch struct {
	Clock   *ClockState   `json:"clock,omitempty"`
	Codex   *CodexState   `json:"codex,omitempty"`
	Agents  *AgentsState  `json:"agents,omitempty"`
	Weather *WeatherState `json:"weather,omitempty"`
	System  *SystemState  `json:"system,omitempty"`
}

type DeviceMessage struct {
	Envelope *Envelope
	ACK      *ACK
}

func NewEnvelope(id string, messageType MessageType, revision uint64, timestamp time.Time, payload any) (Envelope, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("marshal payload: %w", err)
	}
	envelope := Envelope{V: Version, ID: id, Type: messageType, TS: timestamp, Revision: revision, Payload: data}
	if err := envelope.Validate(); err != nil {
		return Envelope{}, err
	}
	return envelope, nil
}

func Decode(data []byte) (Envelope, error) {
	if len(data) == 0 || len(data) > MaxMessageBytes {
		return Envelope{}, errors.New("message size is invalid")
	}
	if !utf8.Valid(data) {
		return Envelope{}, errors.New("message is not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var envelope Envelope
	if err := decoder.Decode(&envelope); err != nil {
		return Envelope{}, fmt.Errorf("decode envelope: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Envelope{}, errors.New("decode envelope: trailing JSON value")
	}
	if err := envelope.Validate(); err != nil {
		return Envelope{}, err
	}
	return envelope, nil
}

func DecodePayload[T any](envelope Envelope) (T, error) {
	var value T
	decoder := json.NewDecoder(bytes.NewReader(envelope.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, fmt.Errorf("decode %s payload: %w", envelope.Type, err)
	}
	return value, nil
}

func DecodeDeviceMessage(data []byte) (DeviceMessage, error) {
	if len(data) == 0 || len(data) > MaxMessageBytes || !utf8.Valid(data) {
		return DeviceMessage{}, errors.New("invalid device message")
	}
	var header struct {
		Type MessageType `json:"type"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return DeviceMessage{}, err
	}
	if header.Type == TypeACK {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		var ack ACK
		if err := decoder.Decode(&ack); err != nil {
			return DeviceMessage{}, err
		}
		if err := ack.Validate(); err != nil {
			return DeviceMessage{}, err
		}
		return DeviceMessage{ACK: &ack}, nil
	}
	envelope, err := Decode(data)
	if err != nil {
		return DeviceMessage{}, err
	}
	return DeviceMessage{Envelope: &envelope}, nil
}

func (envelope Envelope) Validate() error {
	if envelope.V != Version {
		return fmt.Errorf("unsupported protocol version %d", envelope.V)
	}
	if envelope.ID == "" || envelope.TS.IsZero() || len(envelope.Payload) == 0 || bytes.Equal(envelope.Payload, []byte("null")) {
		return errors.New("id, ts and payload are required")
	}
	switch envelope.Type {
	case TypeNotification:
		value, err := DecodePayload[Notification](envelope)
		if err != nil {
			return err
		}
		return value.Validate()
	case TypeHello:
		value, err := DecodePayload[Hello](envelope)
		if err != nil {
			return err
		}
		if value.ProtocolVersion != Version || (value.Role != "server" && value.Role != "device") {
			return errors.New("invalid hello payload")
		}
	case TypeGetSnapshot:
		_, err := DecodePayload[GetSnapshot](envelope)
		return err
	case TypeSnapshot:
		value, err := DecodePayload[Snapshot](envelope)
		if err != nil {
			return err
		}
		return value.Validate()
	case TypeStatePatch:
		value, err := DecodePayload[StatePatch](envelope)
		if err != nil {
			return err
		}
		if value.Clock == nil && value.Codex == nil && value.Agents == nil && value.Weather == nil && value.System == nil {
			return errors.New("empty state patch")
		}
		if value.Agents != nil && value.Agents.CodexActive && !value.Agents.Connected {
			return errors.New("disconnected Herdr state cannot have an active Codex session")
		}
	case TypeHeartbeat, TypeError, TypeDeviceStatus, TypeButtonAction:
		// Direction-specific validation is performed by the endpoint.
	default:
		return fmt.Errorf("unsupported message type %q", envelope.Type)
	}
	return nil
}

func (notification Notification) Validate() error {
	if notification.Kind == "" || notification.Source == "" || notification.SubjectID == "" ||
		notification.DedupeKey == "" || notification.Title == "" || notification.ExpiresAt.IsZero() {
		return errors.New("notification required field is missing")
	}
	if notification.DisplayMS < 1500 || notification.DisplayMS > 12000 || notification.Priority > 100 {
		return errors.New("notification display_ms or priority is out of range")
	}
	if utf8.RuneCountInString(notification.Title) > 28 || utf8.RuneCountInString(notification.Detail) > 64 ||
		utf8.RuneCountInString(notification.SourceLabel) > 12 {
		return errors.New("notification text is too long")
	}
	switch notification.Category {
	case CategoryAgent, CategoryQuota, CategoryWeather, CategorySystem:
	default:
		return fmt.Errorf("invalid notification category %q", notification.Category)
	}
	if !strings.HasPrefix(notification.Kind, string(notification.Category)+".") {
		return errors.New("notification kind does not match category")
	}
	switch notification.Theme {
	case ThemeBlue, ThemeYellow, ThemeRed, ThemeGreen:
	default:
		return fmt.Errorf("invalid notification theme %q", notification.Theme)
	}
	switch notification.Urgency {
	case UrgencyNormal, UrgencyAttention, UrgencyUrgent:
	default:
		return fmt.Errorf("invalid notification urgency %q", notification.Urgency)
	}
	return nil
}

func (ack ACK) Validate() error {
	if ack.V != Version || ack.Type != TypeACK || ack.ID == "" || ack.DeviceID == "" || ack.At.IsZero() {
		return errors.New("invalid ACK envelope")
	}
	switch ack.Status {
	case ACKReceived, ACKQueued, ACKShown, ACKCompleted, ACKInterrupted, ACKSuperseded,
		ACKExpired, ACKDropped, ACKInvalid, ACKDuplicate:
		return nil
	default:
		return fmt.Errorf("invalid ACK status %q", ack.Status)
	}
}

func (snapshot Snapshot) Validate() error {
	if snapshot.Clock.Timezone == "" || snapshot.Clock.ServerTime.IsZero() {
		return errors.New("snapshot clock is required")
	}
	if len(snapshot.Codex.Homes) < 1 || len(snapshot.Codex.Homes) > 2 {
		return errors.New("snapshot must contain one or two Codex homes")
	}
	for _, home := range snapshot.Codex.Homes {
		if home.ID == "" || home.Label == "" || home.WeeklyRemainingPercent < 0 || home.WeeklyRemainingPercent > 100 {
			return errors.New("invalid Codex home")
		}
	}
	if rate := snapshot.Codex.TokenRate.TokensPerSecond; rate != nil && (*rate < 0 || *rate > 10000 || math.IsNaN(*rate) || math.IsInf(*rate, 0)) {
		return errors.New("invalid Codex token rate")
	}
	if snapshot.Codex.TokenRate.ActiveSessions < 0 || snapshot.Codex.TokenRate.ActiveStreams < 0 {
		return errors.New("invalid Codex token-rate activity")
	}
	if !snapshot.Codex.TokenRate.Estimated || snapshot.Codex.TokenRate.ActiveSessions > snapshot.Codex.TokenRate.ActiveStreams ||
		snapshot.Codex.TokenRate.WindowMS > 600000 ||
		(snapshot.Codex.TokenRate.TokensPerSecond == nil &&
			(snapshot.Codex.TokenRate.ActiveSessions != 0 || snapshot.Codex.TokenRate.ActiveStreams != 0)) {
		return errors.New("invalid Codex token-rate contract")
	}
	switch snapshot.Codex.TokenRate.Freshness {
	case FreshnessFresh, FreshnessCached, FreshnessStale, FreshnessUnknown:
	default:
		return errors.New("invalid Codex token-rate freshness")
	}
	if snapshot.Agents.Provider != "herdr" {
		return errors.New("agents provider must be herdr")
	}
	if snapshot.Agents.CodexActive && !snapshot.Agents.Connected {
		return errors.New("disconnected Herdr state cannot have an active Codex session")
	}
	for _, item := range snapshot.Agents.Items {
		switch item.Status {
		case AgentWorking, AgentBlocked, AgentDone, AgentIdle, AgentUnknown:
		default:
			return fmt.Errorf("invalid Herdr status %q", item.Status)
		}
	}
	if snapshot.Weather.Provider != "qweather" || snapshot.Weather.Location == "" {
		return errors.New("invalid weather provider")
	}
	switch snapshot.System.OverallFreshness {
	case FreshnessFresh, FreshnessCached, FreshnessStale, FreshnessUnknown:
	default:
		return errors.New("invalid overall freshness")
	}
	return nil
}
