package sandbox

import (
	"encoding/json"
	"os"
	"time"

	Types "github.com/DicksenT/neurabox/internal/types"
)

func AuditLog(e *Types.AuditEntry) error {
	e.Timestamp = time.Now().Format(time.RFC3339)
	data, _ := json.Marshal(e)
	//move to sqlite later
	f, err := os.OpenFile("audit.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, writeErr := f.Write(append(data, '\n'))
	return writeErr
}
