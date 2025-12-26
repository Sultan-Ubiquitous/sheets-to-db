package cdc

import (
	"fmt"
	"log"
	"os"

	"github.com/go-mysql-org/go-mysql/canal"
	"github.com/go-mysql-org/go-mysql/mysql"
)

type SyncEvent struct {
	Source string
	RowID  string
	Action string
	Data   map[string]any
}

type MyEventHandler struct {
	canal.DummyEventHandler
	OutChan chan<- SyncEvent
}

func StartListener(outChan chan<- SyncEvent, startFile string, startPos uint32) {
	cfg := canal.NewDefaultConfig()

	dbHost := os.Getenv("DB_HOST")
	if dbHost == "" {
		dbHost = "127.0.0.1"
	}

	// Use the dynamic host instead of hardcoded 127.0.0.1
	cfg.Addr = fmt.Sprintf("%s:3306", dbHost)

	cfg.User = "replicator"
	cfg.Password = "password"
	cfg.Dump.ExecutionPath = ""
	cfg.IncludeTableRegex = []string{"interndb\\.product"}

	c, err := canal.NewCanal(cfg)
	if err != nil {
		log.Fatalf("CDC Setup Error: %v", err)
	}

	c.SetEventHandler(&MyEventHandler{OutChan: outChan})

	pos := mysql.Position{
		Name: startFile,
		Pos:  startPos,
	}

	log.Printf("CDC Listener starting from: %s : %d", startFile, startPos)

	if err := c.RunFrom(pos); err != nil {
		log.Fatalf("CDC Run Error: %v", err)
	}
}

func (h *MyEventHandler) OnRow(e *canal.RowsEvent) error {
	if e.Action == canal.UpdateAction || e.Action == canal.InsertAction || e.Action == canal.DeleteAction {

		row := e.Rows[len(e.Rows)-1]

		lastUpdatedBy := "system"
		if len(row) > 6 {
			if val, ok := row[6].(string); ok {
				lastUpdatedBy = val
			} else if valBytes, ok := row[6].([]byte); ok {
				lastUpdatedBy = string(valBytes)
			}
		}

		if lastUpdatedBy == "sync_bot" {
			return nil
		}

		uuid := ""
		if len(row) > 0 {
			if val, ok := row[0].(string); ok {
				uuid = val
			} else if valBytes, ok := row[0].([]byte); ok {
				uuid = string(valBytes)
			}
		}

		data := map[string]interface{}{}

		if len(row) > 4 {
			data["uuid"] = uuid
			data["product_name"] = row[1]
			data["quantity"] = row[2]
			data["price"] = row[3]
			data["discount"] = row[4]
			data["last_updated_by"] = lastUpdatedBy
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
