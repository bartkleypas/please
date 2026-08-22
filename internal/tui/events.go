package tui

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bartkleypas/please/internal/server"
	tea "github.com/charmbracelet/bubbletea"
)

type remoteDaemonEventMsg struct {
	Event server.DaemonEvent
}

type remoteDaemonStreamConnMsg struct {
	EventChan <-chan server.DaemonEvent
	Cancel    context.CancelFunc
}

// listenRemoteEventsCmd establishes a long-running background connection to /api/v1/events
func listenRemoteEventsCmd(remoteURL, authToken, caCertPath string) tea.Cmd {
	return func() tea.Msg {
		if remoteURL == "" {
			return nil
		}

		baseURL := strings.TrimRight(remoteURL, "/")
		if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
			baseURL = "http://" + baseURL
		}

		transport := http.DefaultTransport.(*http.Transport).Clone()
		if caCertPath != "" {
			if caPEM, err := os.ReadFile(caCertPath); err == nil {
				pool := x509.NewCertPool()
				if pool.AppendCertsFromPEM(caPEM) {
					transport.TLSClientConfig = &tls.Config{RootCAs: pool}
				}
			}
		}

		client := &http.Client{Transport: transport}

		ctx, cancel := context.WithCancel(context.Background())
		eventChan := make(chan server.DaemonEvent, 64)

		go func() {
			defer close(eventChan)
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/events", nil)
				if err != nil {
					time.Sleep(2 * time.Second)
					continue
				}

				req.Header.Set("Accept", "text/event-stream")
				if authToken != "" {
					req.Header.Set("Authorization", "Bearer "+authToken)
				}

				resp, err := client.Do(req)
				if err != nil {
					time.Sleep(2 * time.Second)
					continue
				}

				scanner := bufio.NewScanner(resp.Body)
				for scanner.Scan() {
					line := scanner.Text()
					if strings.HasPrefix(line, "data: ") {
						dataStr := strings.TrimPrefix(line, "data: ")
						var ev server.DaemonEvent
						if err := json.Unmarshal([]byte(dataStr), &ev); err == nil {
							select {
							case eventChan <- ev:
							case <-ctx.Done():
								resp.Body.Close()
								return
							}
						}
					}
				}
				resp.Body.Close()
				time.Sleep(1 * time.Second)
			}
		}()

		return remoteDaemonStreamConnMsg{
			EventChan: eventChan,
			Cancel:    cancel,
		}
	}
}

// waitForRemoteEventCmd listens on the active daemon event channel
func waitForRemoteEventCmd(ch <-chan server.DaemonEvent) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return remoteDaemonEventMsg{Event: ev}
	}
}

func (m *Model) handleRemoteDaemonEvent(msg remoteDaemonEventMsg) (tea.Model, tea.Cmd) {
	// Sync local graph with latest mutations from daemon
	_, lastID, err := m.Manager.Sync()
	if err == nil {
		switch msg.Event.Type {
		case server.EventNodeSaved:
			if lastID != "" && (m.CurrentID == "" || m.TextInput.Value() == "") {
				m.CurrentID = lastID
			}
			m.updateViewportContent()
			if m.ViewMode == ModeMap {
				m.syncMapSelection()
			}
		case server.EventBranchPruned, server.EventBranchCompacted:
			m.updateViewportContent()
			if m.ViewMode == ModeMap {
				m.syncMapSelection()
			}
		}
	}

	return m, waitForRemoteEventCmd(m.RemoteEventsChan)
}
