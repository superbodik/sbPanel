package ws

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/yourorg/panel/internal/models"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func subprotocolResponseHeader(r *http.Request) http.Header {
	protocols := websocket.Subprotocols(r)
	if len(protocols) == 0 {
		return nil
	}
	header := http.Header{}
	header.Set("Sec-WebSocket-Protocol", protocols[0])
	return header
}

type consoleSession struct {
	conn    *websocket.Conn
	cancel  context.CancelFunc
	writeMu sync.Mutex
}

type Hub struct {
	mu      sync.RWMutex
	rooms   map[uuid.UUID]map[*websocket.Conn]struct{}
	pollers map[uuid.UUID]context.CancelFunc

	consoleRooms    map[uuid.UUID]map[*websocket.Conn]struct{}
	consoleSessions map[uuid.UUID]*consoleSession

	FetchStats    func(ctx context.Context, serverUUID uuid.UUID) (*models.ResourceStats, error)
	DialConsole   func(ctx context.Context, serverUUID uuid.UUID) (*websocket.Conn, error)
	PersistStatus func(ctx context.Context, serverUUID uuid.UUID, state models.ServerStatus)
}

func NewHub() *Hub {
	return &Hub{
		rooms:           make(map[uuid.UUID]map[*websocket.Conn]struct{}),
		pollers:         make(map[uuid.UUID]context.CancelFunc),
		consoleRooms:    make(map[uuid.UUID]map[*websocket.Conn]struct{}),
		consoleSessions: make(map[uuid.UUID]*consoleSession),
	}
}

func (h *Hub) ServeServerSocket(w http.ResponseWriter, r *http.Request, serverUUID uuid.UUID) {
	conn, err := upgrader.Upgrade(w, r, subprotocolResponseHeader(r))
	if err != nil {
		log.Printf("ws upgrade failed: %v", err)
		return
	}
	defer conn.Close()
	conn.SetReadLimit(4096)

	h.subscribe(serverUUID, conn)
	defer h.unsubscribe(serverUUID, conn)

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (h *Hub) ServeConsoleSocket(w http.ResponseWriter, r *http.Request, serverUUID uuid.UUID) {
	conn, err := upgrader.Upgrade(w, r, subprotocolResponseHeader(r))
	if err != nil {
		log.Printf("console ws upgrade failed: %v", err)
		return
	}
	defer conn.Close()
	conn.SetReadLimit(32 * 1024)

	if err := h.subscribeConsole(serverUUID, conn); err != nil {
		log.Printf("console dial failed: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte("[console] could not reach the node daemon: "+err.Error()))
		return
	}
	defer h.unsubscribeConsole(serverUUID, conn)

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		h.mu.RLock()
		session := h.consoleSessions[serverUUID]
		h.mu.RUnlock()
		if session == nil {
			continue
		}
		session.writeMu.Lock()
		writeErr := session.conn.WriteMessage(websocket.TextMessage, msg)
		session.writeMu.Unlock()
		if writeErr != nil {
			log.Printf("console write to daemon failed: %v", writeErr)
		}
	}
}

func (h *Hub) Broadcast(serverUUID uuid.UUID, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("ws broadcast marshal failed: %v", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for conn := range h.rooms[serverUUID] {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("ws write failed: %v", err)
		}
	}
}

func (h *Hub) broadcastConsole(serverUUID uuid.UUID, data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for conn := range h.consoleRooms[serverUUID] {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("console ws write failed: %v", err)
		}
	}
}

func (h *Hub) subscribe(serverUUID uuid.UUID, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[serverUUID] == nil {
		h.rooms[serverUUID] = make(map[*websocket.Conn]struct{})
	}
	firstSubscriber := len(h.rooms[serverUUID]) == 0
	h.rooms[serverUUID][conn] = struct{}{}

	if firstSubscriber && h.FetchStats != nil {
		ctx, cancel := context.WithCancel(context.Background())
		h.pollers[serverUUID] = cancel
		go h.pollStats(ctx, serverUUID)
	}
}

func (h *Hub) unsubscribe(serverUUID uuid.UUID, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.rooms[serverUUID], conn)
	if len(h.rooms[serverUUID]) == 0 {
		delete(h.rooms, serverUUID)
		if cancel, ok := h.pollers[serverUUID]; ok {
			cancel()
			delete(h.pollers, serverUUID)
		}
	}
}

func (h *Hub) subscribeConsole(serverUUID uuid.UUID, conn *websocket.Conn) error {
	h.mu.Lock()
	if h.consoleRooms[serverUUID] == nil {
		h.consoleRooms[serverUUID] = make(map[*websocket.Conn]struct{})
	}
	firstSubscriber := len(h.consoleRooms[serverUUID]) == 0
	h.consoleRooms[serverUUID][conn] = struct{}{}
	h.mu.Unlock()

	if !firstSubscriber || h.DialConsole == nil {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	daemonConn, err := h.DialConsole(ctx, serverUUID)
	if err != nil {
		cancel()
		h.mu.Lock()
		delete(h.consoleRooms[serverUUID], conn)
		if len(h.consoleRooms[serverUUID]) == 0 {
			delete(h.consoleRooms, serverUUID)
		}
		h.mu.Unlock()
		return err
	}

	session := &consoleSession{conn: daemonConn, cancel: cancel}
	h.mu.Lock()
	h.consoleSessions[serverUUID] = session
	h.mu.Unlock()

	go h.pumpConsole(ctx, serverUUID, session)
	return nil
}

func (h *Hub) unsubscribeConsole(serverUUID uuid.UUID, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.consoleRooms[serverUUID], conn)
	if len(h.consoleRooms[serverUUID]) == 0 {
		delete(h.consoleRooms, serverUUID)
		if session, ok := h.consoleSessions[serverUUID]; ok {
			session.cancel()
			session.conn.Close()
			delete(h.consoleSessions, serverUUID)
		}
	}
}

func (h *Hub) pumpConsole(ctx context.Context, serverUUID uuid.UUID, session *consoleSession) {
	defer session.conn.Close()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		_, msg, err := session.conn.ReadMessage()
		if err != nil {
			return
		}
		h.broadcastConsole(serverUUID, msg)
	}
}

func (h *Hub) pollStats(ctx context.Context, serverUUID uuid.UUID) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats, err := h.FetchStats(ctx, serverUUID)
			if err != nil {
				continue
			}
			h.Broadcast(serverUUID, stats)
			if h.PersistStatus != nil {
				h.PersistStatus(ctx, serverUUID, stats.State)
			}
		}
	}
}
