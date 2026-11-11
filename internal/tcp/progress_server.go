package tcp

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"time"
)

type ProgressUpdate struct {
	Type           string `json:"type"`
	UserID         int    `json:"user_id"`
	MangaID        int    `json:"manga_id"`
	CurrentChapter int    `json:"current_chapter"`
	Status         string `json:"status"`
	UpdatedAt      string `json:"updated_at"`
}

type ClientMessage struct {
	Type   string `json:"type"`
	UserID int    `json:"user_id"`
}

type Broadcaster interface {
	Broadcast(update ProgressUpdate)
}

type registration struct {
	userID int
	conn   net.Conn
}

type ProgressSyncServer struct {
	addr         string
	logger       *log.Logger
	listener     net.Listener
	registerCh   chan registration
	unregisterCh chan registration
	broadcastCh  chan ProgressUpdate
	clients      map[int]map[net.Conn]struct{}
	connIndex    map[net.Conn]int
}

func NewProgressSyncServer(addr string, logger *log.Logger) *ProgressSyncServer {
	if logger == nil {
		logger = log.Default()
	}
	return &ProgressSyncServer{
		addr:         addr,
		logger:       logger,
		registerCh:   make(chan registration, 16),
		unregisterCh: make(chan registration, 16),
		broadcastCh:  make(chan ProgressUpdate, 64),
		clients:      make(map[int]map[net.Conn]struct{}),
		connIndex:    make(map[net.Conn]int),
	}
}

func (s *ProgressSyncServer) Start() error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.listener = listener
	s.logger.Printf("TCP progress sync listening on %s", s.addr)

	go s.run()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			s.logger.Printf("accept error: %v", err)
			continue
		}
		go s.handleConnection(conn)
	}
}

func (s *ProgressSyncServer) Stop() error {
	if s.listener == nil {
		return nil
	}
	return s.listener.Close()
}

func (s *ProgressSyncServer) Broadcast(update ProgressUpdate) {
	select {
	case s.broadcastCh <- update:
	default:
		s.logger.Printf("broadcast channel full for user %d", update.UserID)
	}
}

func (s *ProgressSyncServer) run() {
	for {
		select {
		case reg := <-s.registerCh:
			s.addClient(reg)
		case reg := <-s.unregisterCh:
			s.removeClient(reg)
		case update := <-s.broadcastCh:
			s.broadcast(update)
		}
	}
}

func (s *ProgressSyncServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	var msg ClientMessage
	if err := decoder.Decode(&msg); err != nil {
		s.logger.Printf("register read error from %s: %v", conn.RemoteAddr(), err)
		return
	}
	if msg.Type != "register" || msg.UserID <= 0 {
		s.logger.Printf("invalid register from %s: %+v", conn.RemoteAddr(), msg)
		return
	}

	reg := registration{userID: msg.UserID, conn: conn}
	s.registerCh <- reg
	defer func() {
		s.unregisterCh <- reg
	}()

	for {
		if err := decoder.Decode(&msg); err != nil {
			if !errors.Is(err, io.EOF) {
				s.logger.Printf("read error from %s: %v", conn.RemoteAddr(), err)
			}
			return
		}
	}
}

func (s *ProgressSyncServer) addClient(reg registration) {
	if _, ok := s.clients[reg.userID]; !ok {
		s.clients[reg.userID] = make(map[net.Conn]struct{})
	}
	s.clients[reg.userID][reg.conn] = struct{}{}
	s.connIndex[reg.conn] = reg.userID
	s.logger.Printf("client %s registered for user %d", reg.conn.RemoteAddr(), reg.userID)
}

func (s *ProgressSyncServer) removeClient(reg registration) {
	userID := reg.userID
	if userID == 0 {
		if v, ok := s.connIndex[reg.conn]; ok {
			userID = v
		}
	}
	if userID == 0 {
		return
	}

	if conns, ok := s.clients[userID]; ok {
		delete(conns, reg.conn)
		if len(conns) == 0 {
			delete(s.clients, userID)
		}
	}
	delete(s.connIndex, reg.conn)
	s.logger.Printf("client %s disconnected for user %d", reg.conn.RemoteAddr(), userID)
}

func (s *ProgressSyncServer) broadcast(update ProgressUpdate) {
	conns, ok := s.clients[update.UserID]
	if !ok || len(conns) == 0 {
		return
	}

	payload, err := json.Marshal(update)
	if err != nil {
		s.logger.Printf("failed to marshal update: %v", err)
		return
	}
	payload = append(payload, '\n')

	for conn := range conns {
		if err := writeWithDeadline(conn, payload); err != nil {
			s.logger.Printf("write error to %s: %v", conn.RemoteAddr(), err)
			_ = conn.Close()
			s.removeClient(registration{userID: update.UserID, conn: conn})
		}
	}
}

func writeWithDeadline(conn net.Conn, data []byte) error {
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := conn.Write(data)
	_ = conn.SetWriteDeadline(time.Time{})
	return err
}
