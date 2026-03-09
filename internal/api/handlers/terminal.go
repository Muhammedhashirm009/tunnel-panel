package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins since it's an internal panel tool
	},
}

// TerminalMessage represents messages exchanged over WebSocket between xterm.js and the backend
type TerminalMessage struct {
	Type string `json:"type"`          // "auth", "input", "resize"
	Data string `json:"data,omitempty"`
	Host string `json:"host,omitempty"`
	User string `json:"user,omitempty"`
	Pass string `json:"pass,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

// TerminalHandler handles web terminal WebSocket connections
type TerminalHandler struct{}

// NewTerminalHandler creates a new TerminalHandler
func NewTerminalHandler() *TerminalHandler {
	return &TerminalHandler{}
}

// WebSocket handles the upgrade and terminal proxying
func (h *TerminalHandler) WebSocket(c *gin.Context) {
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[terminal] Failed to upgrade: %v", err)
		return
	}
	defer ws.Close()

	// Wait for the auth message
	_, msgBytes, err := ws.ReadMessage()
	if err != nil {
		log.Printf("[terminal] Auth read error: %v", err)
		return
	}

	var authMsg TerminalMessage
	if err := json.Unmarshal(msgBytes, &authMsg); err != nil {
		ws.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[1;31mInvalid JSON auth payload.\x1b[0m\r\n"))
		return
	}

	if authMsg.Type != "auth" {
		ws.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[1;31mExpected auth message first.\x1b[0m\r\n"))
		return
	}

	// Setup SSH Client
	config := &ssh.ClientConfig{
		User: authMsg.User,
		Auth: []ssh.AuthMethod{
			ssh.Password(authMsg.Pass),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // Ignoring host key for local/internal panel purposes
	}

	ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\n\x1b[1;33mConnecting to %s...\x1b[0m\r\n", authMsg.Host)))

	client, err := ssh.Dial("tcp", authMsg.Host, config)
	if err != nil {
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\n\x1b[1;31mSSH Error: %v\x1b[0m\r\n", err)))
		return
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\n\x1b[1;31mSession Error: %v\x1b[0m\r\n", err)))
		return
	}
	defer session.Close()

	// Setup pseudo-terminal
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	cols := authMsg.Cols
	rows := authMsg.Rows
	if cols == 0 { cols = 80 }
	if rows == 0 { rows = 24 }

	if err := session.RequestPty("xterm", rows, cols, modes); err != nil {
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\n\x1b[1;31mPTY Error: %v\x1b[0m\r\n", err)))
		return
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		return
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		return
	}

	if err := session.Shell(); err != nil {
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\n\x1b[1;31mShell Error: %v\x1b[0m\r\n", err)))
		return
	}

	// Copy from SSH to WebSocket
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stdout.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("[terminal] Stdout read error: %v", err)
				}
				break
			}
			if n > 0 {
				ws.WriteMessage(websocket.TextMessage, buf[:n])
			}
		}
	}()

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if err != nil {
				break
			}
			if n > 0 {
				ws.WriteMessage(websocket.TextMessage, buf[:n])
			}
		}
	}()

	// Read from WebSocket and write to SSH Stdin or Resize PTY
	for {
		_, p, err := ws.ReadMessage()
		if err != nil {
			break
		}

		var msg TerminalMessage
		if err := json.Unmarshal(p, &msg); err == nil {
			switch msg.Type {
			case "input":
				stdin.Write([]byte(msg.Data))
			case "resize":
				if msg.Cols > 0 && msg.Rows > 0 {
					session.WindowChange(msg.Rows, msg.Cols)
				}
			}
		}
	}
}
