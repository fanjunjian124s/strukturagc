/**
 * Standalone signaling server for the Nextcloud Spreed app.
 * Copyright (C) 2017 struktur AG
 *
 * @author Joachim Bauch <bauch@struktur.de>
 *
 * @license GNU AGPL version 3 or any later version
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */
package signaling

import (
	"encoding/json"
	"fmt"
	"net/url"
)

const (
	// Version that must be sent in a "hello" message.
	HelloVersion = "1.0"
)

// ClientMessage is a message that is sent from a client to the server.
type ClientMessage struct {
	// The unique request id (optional).
	Id string `json:"id,omitempty"`

	// The type of the request.
	Type string `json:"type"`

	// Filled for type "hello"
	Hello *HelloClientMessage `json:"hello,omitempty"`

	Bye *ByeClientMessage `json:"bye,omitempty"`

	Room *RoomClientMessage `json:"room,omitempty"`

	Message *MessageClientMessage `json:"message,omitempty"`

	Control *ControlClientMessage `json:"control,omitempty"`
}

func (m *ClientMessage) CheckValid() error {
	switch m.Type {
	case "":
		return fmt.Errorf("type missing")
	case "hello":
		if m.Hello == nil {
			return fmt.Errorf("hello missing")
		} else if err := m.Hello.CheckValid(); err != nil {
			return err
		}
	case "bye":
		// No additional check required.
	case "room":
		if m.Room == nil {
			return fmt.Errorf("room missing")
		} else if err := m.Room.CheckValid(); err != nil {
			return err
		}
	case "message":
		if m.Message == nil {
			return fmt.Errorf("message missing")
		} else if err := m.Message.CheckValid(); err != nil {
			return err
		}
	case "control":
		if m.Control == nil {
			return fmt.Errorf("control missing")
		} else if err := m.Control.CheckValid(); err != nil {
			return err
		}
	}
	return nil
}

func (m *ClientMessage) NewErrorServerMessage(e *Error) *ServerMessage {
	return &ServerMessage{
		Id:    m.Id,
		Type:  "error",
		Error: e,
	}
}

func (m *ClientMessage) NewWrappedErrorServerMessage(e error) *ServerMessage {
	return m.NewErrorServerMessage(NewError("internal_error", e.Error()))
}

// ServerMessage is a message that is sent from the server to a client.
type ServerMessage struct {
	Id string `json:"id,omitempty"`

	Type string `json:"type"`

	Error *Error `json:"error,omitempty"`

	Hello *HelloServerMessage `json:"hello,omitempty"`

	Bye *ByeServerMessage `json:"bye,omitempty"`

	Room *RoomServerMessage `json:"room,omitempty"`

	Message *MessageServerMessage `json:"message,omitempty"`

	Control *ControlServerMessage `json:"control,omitempty"`

	Event *EventServerMessage `json:"event,omitempty"`
}

func (r *ServerMessage) CloseAfterSend(session Session) bool {
	if r.Type == "bye" {
		return true
	}

	if r.Type == "event" {
		if evt := r.Event; evt != nil && evt.Target == "roomlist" && evt.Type == "disinvite" {
			// Only close session / connection if the disinvite was for the room
			// the session is currently in.
			if session != nil && evt.Disinvite != nil {
				if room := session.GetRoom(); room != nil && evt.Disinvite.RoomId == room.Id() {
					return true
				}
			}
		}
	}

	return false
}

func (r *ServerMessage) IsChatRefresh() bool {
	if r.Type != "message" || r.Message == nil || r.Message.Data == nil || len(*r.Message.Data) == 0 {
		return false
	}

	var data MessageServerMessageData
	if err := json.Unmarshal(*r.Message.Data, &data); err != nil {
		return false
	}

	if data.Type != "chat" || data.Chat == nil {
		return false
	}

	return data.Chat.Refresh
}

func (r *ServerMessage) IsParticipantsUpdate() bool {
	if r.Type != "event" || r.Event == nil {
		return false
	}
	if event := r.Event; event.Target != "participants" || event.Type != "update" {
		return false
	}
	return true
}

type Error struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
}

func NewError(code string, message string) *Error {
	return NewErrorDetail(code, message, nil)
}

func NewErrorDetail(code string, message string, details interface{}) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Details: details,
	}
}

func (e *Error) Error() string {
	return e.Message
}

const (
	HelloClientTypeClient   = "client"
	HelloClientTypeInternal = "internal"
)

type ClientTypeInternalAuthParams struct {
	Random string `json:"random"`
	Token  string `json:"token"`
}

type HelloClientMessageAuth struct {
	// The client type that is connecting. Leave empty to use the default
	// "HelloClientTypeClient"
	Type string `json:"type,omitempty"`

	Params *json.RawMessage `json:"params"`

	Url       string `json:"url"`
	parsedUrl *url.URL

	internalParams ClientTypeInternalAuthParams
}

// Type "hello"

type HelloClientMessage struct {
	Version string `json:"version"`

	ResumeId string `json:"resumeid"`

	Features []string `json:"features,omitempty"`

	// The authentication credentials.
	Auth HelloClientMessageAuth `json:"auth"`
}

func (m *HelloClientMessage) CheckValid() error {
	if m.Version != HelloVersion {
		return fmt.Errorf("unsupported hello version: %s", m.Version)
	}
	if m.ResumeId == "" {
		if m.Auth.Params == nil || len(*m.Auth.Params) == 0 {
			return fmt.Errorf("params missing")
		}
		if m.Auth.Type == "" {
			m.Auth.Type = HelloClientTypeClient
		}
		switch m.Auth.Type {
		case HelloClientTypeClient:
			if m.Auth.Url == "" {
				return fmt.Errorf("url missing")
			} else if u, err := url.ParseRequestURI(m.Auth.Url); err != nil {
				return err
			} else {
				m.Auth.parsedUrl = u
			}
		case HelloClientTypeInternal:
			if err := json.Unmarshal(*m.Auth.Params, &m.Auth.internalParams); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported auth type")
		}
	}
	return nil
}

const (
	ServerFeatureMcu = "mcu"
)

type HelloServerMessageServer struct {
	Version  string   `json:"version"`
	Features []string `json:"features,omitempty"`
}

type HelloServerMessage struct {
	Version string `json:"version"`

	SessionId string                    `json:"sessionid"`
	ResumeId  string                    `json:"resumeid"`
	UserId    string                    `json:"userid"`
	Server    *HelloServerMessageServer `json:"server,omitempty"`
}

// Type "bye"

type ByeClientMessage struct {
}

func (m *ByeClientMessage) CheckValid() error {
	// No additional validation required.
	return nil
}

type ByeServerMessage struct {
	Reason string `json:"reason"`
}

// Type "room"

type RoomClientMessage struct {
	RoomId    string `json:"roomid"`
	SessionId string `json:"sessionid,omitempty"`
}

func (m *RoomClientMessage) CheckValid() error {
	// No additional validation required.
	return nil
}

type RoomServerMessage struct {
	RoomId     string           `json:"roomid"`
	Properties *json.RawMessage `json:"properties,omitempty"`
}

// Type "message"

const (
	RecipientTypeSession = "session"
	RecipientTypeUser    = "user"
	RecipientTypeRoom    = "room"
)

type MessageClientMessageRecipient struct {
	Type string `json:"type"`

	SessionId string `json:"sessionid,omitempty"`
	UserId    string `json:"userid,omitempty"`
}

type MessageClientMessage struct {
	Recipient MessageClientMessageRecipient `json:"recipient"`

	Data *json.RawMessage `json:"data"`
}

type MessageClientMessageData struct {
	Type     string                 `json:"type"`
	Sid      string                 `json:"sid"`
	RoomType string                 `json:"roomType"`
	Payload  map[string]interface{} `json:"payload"`
}

func (m *MessageClientMessage) CheckValid() error {
	if m.Data == nil || len(*m.Data) == 0 {
		return fmt.Errorf("message empty")
	}
	switch m.Recipient.Type {
	case RecipientTypeRoom:
		// No additional checks required.
	case RecipientTypeSession:
		if m.Recipient.SessionId == "" {
			return fmt.Errorf("session id missing")
		}
	case RecipientTypeUser:
		if m.Recipient.UserId == "" {
			return fmt.Errorf("user id missing")
		}
	default:
		return fmt.Errorf("unsupported recipient type %v", m.Recipient.Type)
	}
	return nil
}

type MessageServerMessageSender struct {
	Type string `json:"type"`

	SessionId string `json:"sessionid,omitempty"`
	UserId    string `json:"userid,omitempty"`
}

type MessageServerMessageDataChat struct {
	Refresh bool `json:"refresh"`
}

type MessageServerMessageData struct {
	Type string `json:"type"`

	Chat *MessageServerMessageDataChat `json:"chat,omitempty"`
}

type MessageServerMessage struct {
	Sender *MessageServerMessageSender `json:"sender"`

	Data *json.RawMessage `json:"data"`
}

// Type "control"

type ControlClientMessage struct {
	MessageClientMessage
}

type ControlServerMessage struct {
	Sender *MessageServerMessageSender `json:"sender"`

	Data *json.RawMessage `json:"data"`
}

// Type "event"

type RoomEventServerMessage struct {
	RoomId     string           `json:"roomid"`
	Properties *json.RawMessage `json:"properties,omitempty"`
	// TODO(jojo): Change "InCall" to "int" when #914 has landed in NC Talk.
	InCall  *json.RawMessage         `json:"incall,omitempty"`
	Changed []map[string]interface{} `json:"changed,omitempty"`
	Users   []map[string]interface{} `json:"users,omitempty"`
}

type RoomEventMessage struct {
	RoomId string           `json:"roomid"`
	Data   *json.RawMessage `json:"data,omitempty"`
}

type EventServerMessage struct {
	Target string `json:"target"`
	Type   string `json:"type"`

	// Used for target "room"
	Join   []*EventServerMessageSessionEntry `json:"join,omitempty"`
	Leave  []string                          `json:"leave,omitempty"`
	Change []*EventServerMessageSessionEntry `json:"change,omitempty"`

	// Used for target "roomlist" / "participants"
	Invite    *RoomEventServerMessage `json:"invite,omitempty"`
	Disinvite *RoomEventServerMessage `json:"disinvite,omitempty"`
	Update    *RoomEventServerMessage `json:"update,omitempty"`

	// Used for target "message"
	Message *RoomEventMessage `json:"message,omitempty"`
}

type EventServerMessageSessionEntry struct {
	SessionId string           `json:"sessionid"`
	UserId    string           `json:"userid"`
	User      *json.RawMessage `json:"user,omitempty"`
}

// MCU-related types

type AnswerOfferMessage struct {
	To       string                 `json:"to"`
	From     string                 `json:"from"`
	Type     string                 `json:"type"`
	RoomType string                 `json:"roomType"`
	Payload  map[string]interface{} `json:"payload"`
}
