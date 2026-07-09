package ws

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/websocket"
)

func TestSubprotocolHandshakeRoundTrip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protocols := websocket.Subprotocols(r)
		if len(protocols) == 0 || protocols[0] != "test-jwt-token" {
			t.Errorf("server did not receive expected subprotocol, got %v", protocols)
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(w, r, subprotocolResponseHeader(r))
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
		defer conn.Close()
		if err := conn.WriteMessage(websocket.TextMessage, []byte("hello from server")); err != nil {
			t.Errorf("write failed: %v", err)
		}
	}))
	defer server.Close()

	wsURL := "ws" + server.URL[len("http"):]
	dialer := websocket.Dialer{Subprotocols: []string{"test-jwt-token"}}
	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("client dial failed: %v", err)
	}
	defer conn.Close()

	if got := resp.Header.Get("Sec-WebSocket-Protocol"); got != "test-jwt-token" {
		t.Errorf("server did not echo subprotocol in response, got %q", got)
	}

	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(msg) != "hello from server" {
		t.Errorf("unexpected message: %q", msg)
	}
}
