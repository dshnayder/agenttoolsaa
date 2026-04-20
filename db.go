package main

import (
	"context"
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func initDB(filepath string) error {
	var err error
	db, err = sql.Open("sqlite3", filepath)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS chat_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		role TEXT,
		message TEXT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_timestamp ON chat_history(timestamp DESC);
	`)
	return err
}

func saveChatMessage(ctx context.Context, role, message string) error {
	_, err := db.ExecContext(ctx, "INSERT INTO chat_history (role, message) VALUES (?, ?)", role, message)
	return err
}

func getChatHistory(ctx context.Context) ([]ChatMessage, error) {
	rows, err := db.QueryContext(ctx, "SELECT role, message FROM chat_history ORDER BY timestamp DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type chatMsg struct {
		role    string
		message string
	}
	var messages []chatMsg
	for rows.Next() {
		var m chatMsg
		if err := rows.Scan(&m.role, &m.message); err == nil {
			messages = append(messages, m)
		}
	}

	var history []ChatMessage
	// Reverse order back to chronological
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if i == 0 && m.role == "user" {
			// Skip the most recent user message that we JUST inserted to avoid duplication
			continue
		}

		history = append(history, ChatMessage{
			Role: m.role,
			Text: m.message,
		})
	}
	return history, nil
}


func getMessagesToCompact(ctx context.Context, keepCount int) (ids []int, msgs []ChatMessage, err error) {
	var total int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM chat_history").Scan(&total)
	if err != nil || total <= keepCount {
		return nil, nil, err
	}

	deleteCount := total - keepCount

	rows, err := db.QueryContext(ctx, "SELECT id, role, message, timestamp FROM chat_history ORDER BY timestamp ASC, id ASC LIMIT ?", deleteCount)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var role, message, ts string
		if err := rows.Scan(&id, &role, &message, &ts); err == nil {
			ids = append(ids, id)
			msgs = append(msgs, ChatMessage{Role: role, Text: message})
		}
	}
	return ids, msgs, nil
}

func deleteCompactedMessages(ctx context.Context, ids []int) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	for _, id := range ids {
		_, err := tx.Exec("DELETE FROM chat_history WHERE id = ?", id)
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}
