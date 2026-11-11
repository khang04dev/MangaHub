package websocket

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func ServeWS(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade error: %v", err)
		return
	}

	username := r.URL.Query().Get("username")
	if username == "" {
		username = "Anonymous"
	}
	if err := ValidateUsername(username); err != nil {
		errMsg, _ := json.Marshal(Message{Type: "error", Content: err.Error()})
		_ = conn.WriteMessage(websocket.TextMessage, errMsg)
		_ = conn.Close()
		return
	}

	room := r.URL.Query().Get("room")
	if room == "" {
		room = "general"
	}

	client := &Client{
		hub:      hub,
		conn:     conn,
		send:     make(chan []byte, 256),
		username: username,
		room:     room,
	}

	client.hub.register <- client

	go client.writePump()
	go client.readPump()
}
