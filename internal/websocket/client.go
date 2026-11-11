package websocket

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
)

type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	username string
	room     string
}

func (c *Client) queueMessage(msg Message) {
	msg.Room = c.room
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("marshal message failed: %v", err)
		return
	}

	select {
	case c.send <- data:
	default:
		c.hub.unregister <- c
	}
}

func (c *Client) queueError(content string) {
	c.queueMessage(Message{Type: "error", Content: content})
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, rawMessage, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("unexpected close for %s: %v", c.username, err)
			}
			break
		}

		var msg Message
		if err := json.Unmarshal(rawMessage, &msg); err != nil {
			c.queueError("invalid JSON format")
			continue
		}

		if err := ValidateMessage(&msg); err != nil {
			c.queueError(err.Error())
			continue
		}

		msg.Sender = c.username
		msg.Timestamp = time.Now().Format(time.RFC3339)

		switch msg.Type {
		case "chat":
			msg.Room = c.room
			out, err := json.Marshal(msg)
			if err != nil {
				log.Printf("marshal chat message failed: %v", err)
				continue
			}
			c.hub.broadcast <- &RoomMessage{room: c.room, message: out}
			log.Printf("[CHAT] %s@%s: %s", c.username, c.room, msg.Content)

		case "join_room":
			newRoom := strings.TrimSpace(msg.Content)
			if newRoom == "" {
				c.queueError("room name is required")
				continue
			}
			oldRoom := c.room
			if err := c.hub.MoveClientToRoom(c, newRoom); err != nil {
				c.queueError(err.Error())
				continue
			}
			c.queueMessage(Message{Type: "system", Sender: "system", Content: "You joined room: " + c.room})
			log.Printf("[JOIN_ROOM] %s moved from %q to %q", c.username, oldRoom, c.room)

		case "list_rooms":
			summary := c.hub.ListRoomSummary()
			c.queueMessage(Message{Type: "room_list", Sender: "system", Content: summary})

		case "list_users":
			users := c.hub.ListUsersInRoom(c.room)
			c.queueMessage(Message{Type: "user_list", Sender: "system", Content: strings.Join(users, ", ")})

		case "dm":
			if msg.Recipient == c.username {
				c.queueError("cannot DM yourself")
				continue
			}
			msg.Room = c.room
			out, err := json.Marshal(msg)
			if err != nil {
				log.Printf("marshal dm message failed: %v", err)
				continue
			}
			c.hub.directMsg <- &DirectMessage{sender: c, recipient: msg.Recipient, message: out}
			log.Printf("[DM] %s -> %s: %s", c.username, msg.Recipient, msg.Content)
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
