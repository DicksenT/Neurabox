package sandbox

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"time"

	Types "github.com/DicksenT/neurabox/internal/types"
)

func (m *Manager) AuditLog(e *Types.AuditEntry) error {
	e.Timestamp = time.Now().Format(time.RFC3339)
	data, _ := json.Marshal(e)
	//move to sqlite later
	f, err := os.OpenFile("audit.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY,0644)
	if(err!=nil){
		return err
	}
	defer f.Close()
	_,writeErr := f.Write(append(data, '\n'))
	return writeErr
}

func (m *Manager) SendToSupabase(e *Types.AuditEntry) {
	// 🛡️ REMINDER: Add .env to your block_files list!
	// Use your real project URL + /rest/v1/audit_logs
	supabaseURL := "https://issfimdqgycogoanonjz.supabase.co/rest/v1/audit_logs"
	supabaseKey := "sb_publishable_123c-VBviC2hbkhj0M2fjQ_B7PGRvKZ" // Use your actual Anon Key

	// We use a Goroutine so the user doesn't feel the network lag
	go func(entry Types.AuditEntry) {
		// Map Go struct to SQL Columns exactly
		payload := map[string]interface{}{
			"session_id": entry.ID,
			"user_id":    entry.User,
			"agent":      entry.Agent,
			"prompt":     entry.Prompt,
			"files":      entry.Files, // Go slices automatically marshal to JSONB
			"approved":   entry.Approved,
			"test_pass":  entry.TestPass,
		}

		jsonData, err := json.Marshal(payload)
		if err != nil {
			return 
		}

		req, err := http.NewRequest("POST", supabaseURL, bytes.NewBuffer(jsonData))
		if err != nil {
			return
		}

		// Required Headers for Supabase
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("apikey", supabaseKey)
		req.Header.Set("Authorization", "Bearer "+supabaseKey)
		
		// "return=minimal" makes the request faster by not asking for the row back
		req.Header.Set("Prefer", "return=minimal")

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()

		// Optional: If you see 404/401 during testing, uncomment this once:
		// if resp.StatusCode >= 400 {
		//    fmt.Printf("Telemetry Debug: %d\n", resp.StatusCode)
		// }
	}(*e) // Pass a copy to avoid race conditions
}