package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
	"github.com/yanjiulab/netnslab/internal/netns"
)

type terminalResizeMsg struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func terminalWSHandler(labFilter string) http.HandlerFunc {
	upgrader := websocket.Upgrader{
		// UI is expected to be bound to loopback; keep it simple.
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// Expected path:
		//   /ws/labs/{lab}/nodes/{node}/terminal
		p := strings.TrimPrefix(r.URL.Path, "/ws/labs/")
		p = strings.TrimSuffix(p, "/")
		parts := strings.Split(p, "/")
		if len(parts) != 4 || parts[1] != "nodes" || parts[3] != "terminal" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		labName := parts[0]
		nodeName := parts[2]

		if labFilter != "" && labName != labFilter {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		nsName := netns.NamespaceName(labName, nodeName)

		nodeEnv, err := netns.ReadNodeEnvFile(labName, nodeName)
		if err != nil {
			_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("read node env failed: %v", err)))
			return
		}

		env := netns.MergeEnviron(os.Environ(), nodeEnv)
		env = append(env,
			"TERM=xterm-256color",
			"PS1=netnslab-"+nodeName+":$ ",
		)

		// Start an interactive login shell.
		cmd := exec.Command("ip", "netns", "exec", nsName, "bash", "-l")
		cmd.Env = env
		cmd.Stderr = nil
		cmd.Stdout = nil
		cmd.Stdin = nil

		ptyFile, err := pty.Start(cmd)
		if err != nil {
			_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("pty start failed: %v", err)))
			return
		}
		defer func() { _ = ptyFile.Close() }()

		// Default size; client will resize immediately after connect.
		_ = pty.Setsize(ptyFile, &pty.Winsize{Cols: 120, Rows: 40})

		var once sync.Once
		closeCmd := func() {
			once.Do(func() {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			})
		}
		defer closeCmd()

		// PTY -> websocket
		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := ptyFile.Read(buf)
				if n > 0 {
					if werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
						return
					}
				}
				if err != nil {
					return
				}
			}
		}()

		// websocket -> PTY
		for {
			mt, msg, err := ws.ReadMessage()
			if err != nil {
				return
			}
			switch mt {
			case websocket.BinaryMessage:
				_, _ = ptyFile.Write(msg)
			case websocket.TextMessage:
				var rm terminalResizeMsg
				if err := json.Unmarshal(msg, &rm); err != nil {
					continue
				}
				if rm.Type == "resize" && rm.Cols > 0 && rm.Rows > 0 {
					_ = pty.Setsize(ptyFile, &pty.Winsize{Cols: uint16(rm.Cols), Rows: uint16(rm.Rows)})
				}
			}
		}
	}
}

func hostTerminalWSHandler() http.HandlerFunc {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/host/terminal" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		env := append(os.Environ(),
			"TERM=xterm-256color",
			"PS1=netnslab-host:$ ",
		)

		cmd := exec.Command("bash", "-l")
		cmd.Env = env
		cmd.Stderr = nil
		cmd.Stdout = nil
		cmd.Stdin = nil

		ptyFile, err := pty.Start(cmd)
		if err != nil {
			_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("pty start failed: %v", err)))
			return
		}
		defer func() { _ = ptyFile.Close() }()

		_ = pty.Setsize(ptyFile, &pty.Winsize{Cols: 120, Rows: 40})

		var once sync.Once
		closeCmd := func() {
			once.Do(func() {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			})
		}
		defer closeCmd()

		// PTY -> websocket
		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := ptyFile.Read(buf)
				if n > 0 {
					if werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
						return
					}
				}
				if err != nil {
					return
				}
			}
		}()

		// websocket -> PTY
		for {
			mt, msg, err := ws.ReadMessage()
			if err != nil {
				return
			}
			switch mt {
			case websocket.BinaryMessage:
				_, _ = ptyFile.Write(msg)
			case websocket.TextMessage:
				var rm terminalResizeMsg
				if err := json.Unmarshal(msg, &rm); err != nil {
					continue
				}
				if rm.Type == "resize" && rm.Cols > 0 && rm.Rows > 0 {
					_ = pty.Setsize(ptyFile, &pty.Winsize{Cols: uint16(rm.Cols), Rows: uint16(rm.Rows)})
				}
			}
		}
	}
}
