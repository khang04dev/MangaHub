package websocket

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
)

type Hub struct {
	rooms      map[string]map[*Client]bool
	usernames  map[string]*Client
	broadcast  chan *RoomMessage
	register   chan *Client
	unregister chan *Client
	moveRoom   chan *RoomChangeRequest
	listRooms  chan *RoomListRequest
	listUsers  chan *UserListRequest
	directMsg  chan *DirectMessage
}

type RoomMessage struct {
	room    string
	message []byte
}

type RoomChangeRequest struct {
	client  *Client
	newRoom string
	done    chan error
}

type RoomListRequest struct {
	response chan string
}

type UserListRequest struct {
	room     string
	response chan []string
}

type DirectMessage struct {
	sender    *Client
	recipient string
	message   []byte
}

func NewHub() *Hub {
	return &Hub{
		rooms:      make(map[string]map[*Client]bool),
		usernames:  make(map[string]*Client),
		broadcast:  make(chan *RoomMessage),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		moveRoom:   make(chan *RoomChangeRequest),
		listRooms:  make(chan *RoomListRequest),
		listUsers:  make(chan *UserListRequest),
		directMsg:  make(chan *DirectMessage),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			if _, taken := h.usernames[client.username]; taken {
				errMsg, _ := json.Marshal(Message{Type: "error", Content: "username already taken"})
				select {
				case client.send <- errMsg:
				default:
				}
				close(client.send)
				_ = client.conn.Close()
				continue
			}

			h.usernames[client.username] = client
			h.ensureRoom(client.room)
			h.rooms[client.room][client] = true
			h.broadcastEvent(client.room, Message{Type: "join", Sender: client.username, Content: "joined the chat"})
			log.Printf("[CONNECT] %s joined room %q", client.username, client.room)

		case client := <-h.unregister:
			room := client.room
			delete(h.usernames, client.username)
			if clients, ok := h.rooms[room]; ok {
				if _, exists := clients[client]; exists {
					delete(clients, client)
					if len(clients) == 0 {
						delete(h.rooms, room)
					}
					h.broadcastEvent(room, Message{Type: "leave", Sender: client.username, Content: "left the chat"})
					close(client.send)
					log.Printf("[DISCONNECT] %s left room %q", client.username, room)
				}
			}

		case roomMsg := <-h.broadcast:
			h.queueToRoom(roomMsg.room, roomMsg.message)

		case req := <-h.moveRoom:
			if req.newRoom == "" {
				req.done <- fmt.Errorf("room name is required")
				continue
			}
			if req.client.room == req.newRoom {
				req.done <- nil
				continue
			}

			oldRoom := req.client.room
			h.removeClientFromRoom(req.client, oldRoom)
			h.broadcastEvent(oldRoom, Message{Type: "leave", Sender: req.client.username, Content: "left the room"})

			req.client.room = req.newRoom
			h.ensureRoom(req.client.room)
			h.rooms[req.client.room][req.client] = true
			h.broadcastEvent(req.client.room, Message{Type: "join", Sender: req.client.username, Content: "joined the room"})

			log.Printf("[MOVE] %s moved from %q to %q", req.client.username, oldRoom, req.client.room)
			req.done <- nil

		case req := <-h.listRooms:
			req.response <- h.formatRoomList()

		case req := <-h.listUsers:
			users := h.listUsersInRoom(req.room)
			req.response <- users

		case dm := <-h.directMsg:
			recipient, ok := h.usernames[dm.recipient]
			if !ok {
				errMsg, _ := json.Marshal(Message{Type: "error", Content: "user not found: " + dm.recipient})
				select {
				case dm.sender.send <- errMsg:
				default:
				}
				continue
			}
			select {
			case recipient.send <- dm.message:
			default:
				close(recipient.send)
				delete(h.usernames, recipient.username)
			}
		}
	}
}

func (h *Hub) ensureRoom(room string) {
	if _, ok := h.rooms[room]; !ok {
		h.rooms[room] = make(map[*Client]bool)
	}
}

func (h *Hub) queueToRoom(room string, message []byte) {
	clients, ok := h.rooms[room]
	if !ok {
		return
	}

	for client := range clients {
		select {
		case client.send <- message:
		default:
			close(client.send)
			delete(clients, client)
			delete(h.usernames, client.username)
		}
	}

	if len(clients) == 0 {
		delete(h.rooms, room)
	}
}

func (h *Hub) broadcastEvent(room string, msg Message) {
	msg.Room = room
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("marshal event error: %v", err)
		return
	}
	h.queueToRoom(room, data)
}

func (h *Hub) removeClientFromRoom(client *Client, room string) {
	clients, ok := h.rooms[room]
	if !ok {
		return
	}

	delete(clients, client)
	if len(clients) == 0 {
		delete(h.rooms, room)
	}
}

func (h *Hub) formatRoomList() string {
	if len(h.rooms) == 0 {
		return "no active rooms"
	}

	roomNames := make([]string, 0, len(h.rooms))
	for room := range h.rooms {
		roomNames = append(roomNames, room)
	}
	sort.Strings(roomNames)

	parts := make([]string, 0, len(roomNames))
	for _, room := range roomNames {
		parts = append(parts, fmt.Sprintf("%s (%d)", room, len(h.rooms[room])))
	}

	return strings.Join(parts, ", ")
}

func (h *Hub) MoveClientToRoom(client *Client, newRoom string) error {
	done := make(chan error)
	h.moveRoom <- &RoomChangeRequest{client: client, newRoom: newRoom, done: done}
	return <-done
}

func (h *Hub) ListRoomSummary() string {
	response := make(chan string)
	h.listRooms <- &RoomListRequest{response: response}
	return <-response
}

func (h *Hub) ListUsersInRoom(room string) []string {
	response := make(chan []string)
	h.listUsers <- &UserListRequest{room: room, response: response}
	return <-response
}

func (h *Hub) listUsersInRoom(room string) []string {
	clients := h.rooms[room]
	users := make([]string, 0, len(clients))
	for c := range clients {
		users = append(users, c.username)
	}
	sort.Strings(users)
	return users
}
