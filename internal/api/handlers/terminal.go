package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/Muhammedhashirm009/tunnel-panel/internal/terminal"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true // auth is enforced at the route level
	},
}

// TerminalHandler handles WebSocket PTY terminal sessions
type TerminalHandler struct{}

// NewTerminalHandler creates a new TerminalHandler
func NewTerminalHandler() *TerminalHandler {
	return &TerminalHandler{}
}

// resizeMsg is sent from the client to resize the terminal
type resizeMsg struct {
	Type string `json:"type"`
	Rows uint16 `json:"rows"`
	Cols uint16 `json:"cols"`
}

// HandleWebSocket upgrades the connection and bridges it to a bash PTY
func (h *TerminalHandler) HandleWebSocket(c *gin.Context) {
	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[terminal] WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	sess, err := terminal.SpawnShell()
	if err != nil {
		log.Printf("[terminal] Failed to spawn shell: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[31mFailed to start shell: "+err.Error()+"\x1b[0m\r\n"))
		return
	}
	defer sess.Close()

	log.Printf("[terminal] New terminal session started (PID: %d)", sess.Cmd.Process.Pid)

	done := make(chan struct{})

	// PTY → WebSocket: forward shell output to browser
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := sess.PTY.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("[terminal] PTY read error: %v", err)
				}
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return
			}
		}
	}()

	// WebSocket → PTY: forward browser input/resize to shell
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			break
		}

		select {
		case <-done:
			return
		default:
		}

		if msgType == websocket.TextMessage {
			// Try to parse as a control message (resize)
			var ctrl resizeMsg
			if json.Unmarshal(data, &ctrl) == nil && ctrl.Type == "resize" && ctrl.Rows > 0 && ctrl.Cols > 0 {
				if err := sess.Resize(ctrl.Rows, ctrl.Cols); err != nil {
					log.Printf("[terminal] Resize error: %v", err)
				}
				continue
			}
			// Otherwise treat as raw input
			sess.PTY.Write(data)
		} else if msgType == websocket.BinaryMessage {
			sess.PTY.Write(data)
		}
	}

	<-done
	log.Printf("[terminal] Terminal session ended (PID: %d)", sess.Cmd.Process.Pid)
}
