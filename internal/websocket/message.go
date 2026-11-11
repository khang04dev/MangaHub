package websocket

import (
	"fmt"
	"regexp"
)

type Message struct {
	Type      string `json:"type"`
	Sender    string `json:"sender"`
	Content   string `json:"content"`
	Recipient string `json:"recipient,omitempty"`
	Room      string `json:"room,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]{3,20}$`)

func ValidateUsername(username string) error {
	if username == "" {
		return fmt.Errorf("username is required")
	}
	if !usernameRegex.MatchString(username) {
		return fmt.Errorf("username must be 3-20 alphanumeric characters or underscores")
	}
	return nil
}

func ValidateMessage(msg *Message) error {
	switch msg.Type {
	case "chat":
		if msg.Content == "" {
			return fmt.Errorf("content is required")
		}
	case "dm":
		if msg.Recipient == "" {
			return fmt.Errorf("recipient is required")
		}
		if msg.Content == "" {
			return fmt.Errorf("content is required")
		}
	case "join_room":
		if msg.Content == "" {
			return fmt.Errorf("room name is required")
		}
	case "list_rooms", "list_users":
		return nil
	default:
		return fmt.Errorf("unknown message type: %s", msg.Type)
	}

	if len(msg.Content) > 500 {
		return fmt.Errorf("content exceeds 500 character limit")
	}
	return nil
}
