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
		phone_number TEXT,
		role TEXT,
		message TEXT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_phone ON chat_history(phone_number, timestamp DESC);
	`)
	return err
}

func saveChatMessage(ctx context.Context, phoneStr, role, message string) error {
	_, err := db.ExecContext(ctx, "INSERT INTO chat_history (phone_number, role, message) VALUES (?, ?, ?)", phoneStr, role, message)
	return err
}

func getChatHistory(ctx context.Context, phoneStr string) ([]ChatMessage, error) {
	rows, err := db.QueryContext(ctx, "SELECT role, message FROM chat_history WHERE phone_number = ? ORDER BY timestamp DESC LIMIT 50", phoneStr)
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
