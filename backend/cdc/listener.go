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
	if e.Action == canal.UpdateAction || e.Action == canal.InsertAction || e.Action == canal.DeleteAction {
		// Get the latest row state
		row := e.Rows[len(e.Rows)-1]

		// SQL Schema Mapping:
		// 0: uuid
		// 1: product_name
		// 2: quantity
		// 3: price
		// 4: discount
		// 5: updated_at
		// 6: last_updated_by

		// 1. Extract 'last_updated_by' safely
		lastUpdatedBy := "system" // Default
		if len(row) > 6 {
			if val, ok := row[6].(string); ok {
				lastUpdatedBy = val
			} else if valBytes, ok := row[6].([]byte); ok {
				lastUpdatedBy = string(valBytes)
			}
		}

		// 2. Loop Prevention
		// If the DB update was caused by the bot itself, don't sync back to sheets
		if lastUpdatedBy == "sync_bot" {
			return nil
		}

		// 3. Extract UUID
		uuid := ""
		if len(row) > 0 {
			if val, ok := row[0].(string); ok {
				uuid = val
			} else if valBytes, ok := row[0].([]byte); ok {
				uuid = string(valBytes)
			}
		}

		// 4. Construct Data Map (THE FIX IS HERE)
		// We must explicitly add every field we want to appear in the Sheet.
		data := map[string]interface{}{}

		if len(row) > 4 {
			data["uuid"] = uuid
			data["product_name"] = row[1]
			data["quantity"] = row[2]
			data["price"] = row[3]
			data["discount"] = row[4]               // Added Missing Field
			data["last_updated_by"] = lastUpdatedBy // Added Missing Field
		}

		h.OutChan <- SyncEvent{
			Source: "MYSQL",
			RowID:  uuid,
			Action: e.Action,
			Data:   data,
		}
	}
	return nil
}
