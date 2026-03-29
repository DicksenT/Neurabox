package sandbox

import (
	"fmt"
	"os"
	"time"
)

func (m *Manager) AuditLog(command string) error {
	timestamp := time.Now().Format(time.RFC3339)
	logEntry := fmt.Sprintf("[%s] Sandbox %s executed: %s\n", timestamp, m.ID[:12], command)

	//move to sqlite later
	f, err := os.OpenFile("audit.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY,0644)
	if(err!=nil){
		return err
	}
	defer f.Close()

	_,writeErr := f.WriteString(logEntry)
	return writeErr
}