package cdc

import (
	"log"

	"github.com/go-mysql-org/go-mysql/canal"
	"github.com/go-mysql-org/go-mysql/mysql"
)

// FIX 1: Update Struct Definition to match usage
type SyncEvent struct {
	Source string // Was 'Table' or missing
	RowID  string // Was missing
	Action string // Useful to keep
	Data   map[string]any
}

type MyEventHandler struct {
	canal.DummyEventHandler
	OutChan chan<- SyncEvent
}

func StartListener(outChan chan<- SyncEvent, startFile string, startPos uint32) {
	cfg := canal.NewDefaultConfig()
	cfg.Addr = "127.0.0.1:3306"
	cfg.User = "replicator"
	cfg.Password = "password"
	cfg.Dump.ExecutionPath = "" // We handle initial dump manually, so disable this
	cfg.IncludeTableRegex = []string{"interndb\\.product"}

	c, err := canal.NewCanal(cfg)
	if err != nil {
		log.Fatalf("CDC Setup Error: %v", err)
	}

	c.SetEventHandler(&MyEventHandler{OutChan: outChan})

	// CRITICAL CHANGE: Start from the specific position we captured!
	pos := mysql.Position{
		Name: startFile,
		Pos:  startPos,
	}

	log.Printf("CDC Listener starting from: %s : %d", startFile, startPos)

	// Use RunFrom instead of Run
	if err := c.RunFrom(pos); err != nil {
		log.Fatalf("CDC Run Error: %v", err)
	}
}

func (h *MyEventHandler) OnRow(e *canal.RowsEvent) error {
	if e.Action == canal.UpdateAction || e.Action == canal.InsertAction {
		// Get the latest row state
		row := e.Rows[len(e.Rows)-1]

		// FIX 2: Safe Type Assertion for last_updated_by (Col Index 6 in your SQL schema)
		// Note: Based on your SQL, last_updated_by is the LAST column.
		// 0:uuid, 1:name, 2:quantity, 3:price, 4:discount, 5:updated_at, 6:last_updated_by

		lastUpdatedBy := ""
		if val, ok := row[6].(string); ok {
			lastUpdatedBy = val
		} else if valBytes, ok := row[6].([]byte); ok {
			lastUpdatedBy = string(valBytes)
		}

		// Loop Prevention
		if lastUpdatedBy == "sync_bot" {
			return nil
		}

		// FIX 3: Safe UUID Extraction
		uuid := ""
		if val, ok := row[0].(string); ok {
			uuid = val
		} else if valBytes, ok := row[0].([]byte); ok {
			uuid = string(valBytes)
		}

		data := map[string]interface{}{
			"uuid":         uuid,
			"product_name": row[1],
			"quantity":     row[2],
			"price":        row[3],
		}

		h.OutChan <- SyncEvent{
			Source: "MYSQL",  // Matches Struct
			RowID:  uuid,     // Matches Struct
			Action: e.Action, // Matches Struct
			Data:   data,     // Matches Struct
		}
	}
	return nil
}
