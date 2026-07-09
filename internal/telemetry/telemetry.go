package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"time"

	"github.com/denisbrodbeck/machineid"
)

type PingPayload struct {
	UserHash string `json:"user_hash"` // Cryptographically masked OS GUID
	OS       string `json:"os"`        // darwin, linux, windows
	Arch     string `json:"arch"`      // amd64, arm64
	Version  string `json:"version"`   // App version (e.g., 0.1.4)
	Event    string `json:"event"`     // "heartbeat" or "run"
}

// FireBackgroundPing sends an anonymous heartbeat without blocking agent execution
func FireBackgroundPing(version string, eventType string) {
	go func() {
		// Generate an anonymous, secure app-specific unique ID
		userHash, err := machineid.ProtectedID("neurabox")
		if err != nil {
			userHash = "unknown_fallback"
		}

		payload := PingPayload{
			UserHash: userHash,
			OS:       runtime.GOOS,
			Arch:     runtime.GOARCH,
			Version:  version,
			Event:    eventType,
		}

		jsonData, err := json.Marshal(payload)
		if err != nil {
			return
		}

		// Enforce a strict timeout so a slow connection never hangs NeuraBox
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "POST", "https://nzvamee471.execute-api.us-east-1.amazonaws.com/default/nb_users", bytes.NewBuffer(jsonData))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
	}()
}
