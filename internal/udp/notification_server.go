package udp

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"sync"
	"time"
)

type ChapterRelease struct {
	Type       string `json:"type"`
	MangaID    int    `json:"manga_id"`
	Title      string `json:"title"`
	Chapter    int    `json:"chapter"`
	ReleasedAt string `json:"released_at"`
}

type ClientMessage struct {
	Type   string `json:"type"`
	UserID int    `json:"user_id"`
}

type Notifier interface {
	Notify(userIDs []int, release ChapterRelease)
}

type NotificationServer struct {
	addr    string
	logger  *log.Logger
	conn    *net.UDPConn
	mu      sync.RWMutex
	clients map[int]map[string]*net.UDPAddr
}

func NewNotificationServer(addr string, logger *log.Logger) *NotificationServer {
	if logger == nil {
		logger = log.Default()
	}
	return &NotificationServer{
		addr:    addr,
		logger:  logger,
		clients: make(map[int]map[string]*net.UDPAddr),
	}
}

func (s *NotificationServer) Start() error {
	udpAddr, err := net.ResolveUDPAddr("udp", s.addr)
	if err != nil {
		return err
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return err
	}
	s.conn = conn

	s.logger.Printf("UDP notification server listening on %s", s.addr)

	buffer := make([]byte, 2048)
	for {
		n, clientAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			s.logger.Printf("UDP read error: %v", err)
			continue
		}
		s.handleMessage(clientAddr, buffer[:n])
	}
}

func (s *NotificationServer) Stop() error {
	if s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

func (s *NotificationServer) Notify(userIDs []int, release ChapterRelease) {
	if s.conn == nil {
		return
	}
	if release.Type == "" {
		release.Type = "chapter_release"
	}

	payload, err := json.Marshal(release)
	if err != nil {
		s.logger.Printf("UDP marshal error: %v", err)
		return
	}

	targets := s.collectTargets(userIDs)
	if len(targets) == 0 {
		return
	}

	for _, addr := range targets {
		_ = s.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
		if _, err := s.conn.WriteToUDP(payload, addr); err != nil {
			s.logger.Printf("UDP write error to %s: %v", addr, err)
		}
	}
	_ = s.conn.SetWriteDeadline(time.Time{})
}

func (s *NotificationServer) handleMessage(addr *net.UDPAddr, payload []byte) {
	var msg ClientMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		s.logger.Printf("UDP invalid message from %s: %v", addr, err)
		return
	}

	switch msg.Type {
	case "register":
		if msg.UserID <= 0 {
			return
		}
		s.registerClient(msg.UserID, addr)
	case "unregister":
		if msg.UserID <= 0 {
			return
		}
		s.unregisterClient(msg.UserID, addr)
	default:
		s.logger.Printf("UDP unknown message type %q from %s", msg.Type, addr)
	}
}

func (s *NotificationServer) registerClient(userID int, addr *net.UDPAddr) {
	key := addr.String()

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.clients[userID]; !ok {
		s.clients[userID] = make(map[string]*net.UDPAddr)
	}
	s.clients[userID][key] = addr
	s.logger.Printf("UDP client registered: user=%d addr=%s", userID, key)
}

func (s *NotificationServer) unregisterClient(userID int, addr *net.UDPAddr) {
	key := addr.String()

	s.mu.Lock()
	defer s.mu.Unlock()

	if entries, ok := s.clients[userID]; ok {
		delete(entries, key)
		if len(entries) == 0 {
			delete(s.clients, userID)
		}
	}
	s.logger.Printf("UDP client unregistered: user=%d addr=%s", userID, key)
}

func (s *NotificationServer) collectTargets(userIDs []int) []*net.UDPAddr {
	s.mu.RLock()
	defer s.mu.RUnlock()

	unique := make(map[string]*net.UDPAddr)

	if len(userIDs) == 0 {
		for _, entries := range s.clients {
			for key, addr := range entries {
				unique[key] = addr
			}
		}
	} else {
		for _, userID := range userIDs {
			if entries, ok := s.clients[userID]; ok {
				for key, addr := range entries {
					unique[key] = addr
				}
			}
		}
	}

	result := make([]*net.UDPAddr, 0, len(unique))
	for _, addr := range unique {
		result = append(result, addr)
	}

	return result
}
