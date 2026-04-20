package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// We need a mutex to prevent concurrent writes to the file.
var historyMutex sync.Mutex
var historyFilePath string

func initDB(path string) error {
	historyFilePath = path
	// Ensure directory exists
	dir := filepath.Dir(historyFilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	
	// Create file if it doesn't exist
	if _, err := os.Stat(historyFilePath); os.IsNotExist(err) {
		return os.WriteFile(historyFilePath, []byte("[]"), 0644)
	}
	return nil
}

func saveChatMessage(ctx context.Context, role, message string) error {
	historyMutex.Lock()
	defer historyMutex.Unlock()

	data, err := os.ReadFile(historyFilePath)
	if err != nil {
		return err
	}

	var history []ChatMessage
	if err := json.Unmarshal(data, &history); err != nil {
		return err
	}

	history = append(history, ChatMessage{Role: role, Text: message})

	newData, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(historyFilePath, newData, 0644)
}

func getChatHistory(ctx context.Context) ([]ChatMessage, error) {
	historyMutex.Lock()
	defer historyMutex.Unlock()

	data, err := os.ReadFile(historyFilePath)
	if err != nil {
		return nil, err
	}

	var history []ChatMessage
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, err
	}

	return history, nil
}

func getMessagesToCompact(ctx context.Context, keepCount int) (ids []int, msgs []ChatMessage, err error) {
	historyMutex.Lock()
	defer historyMutex.Unlock()

	data, err := os.ReadFile(historyFilePath)
	if err != nil {
		return nil, nil, err
	}

	var history []ChatMessage
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, nil, err
	}

	if len(history) <= keepCount {
		return nil, nil, nil
	}

	deleteCount := len(history) - keepCount
	
	ids = make([]int, deleteCount)
	for i := 0; i < deleteCount; i++ {
		ids[i] = i
		msgs = append(msgs, history[i])
	}

	return ids, msgs, nil
}

func deleteCompactedMessages(ctx context.Context, ids []int) error {
	historyMutex.Lock()
	defer historyMutex.Unlock()

	data, err := os.ReadFile(historyFilePath)
	if err != nil {
		return err
	}

	var history []ChatMessage
	if err := json.Unmarshal(data, &history); err != nil {
		return err
	}

	if len(ids) > 0 && len(ids) <= len(history) {
		history = history[len(ids):]
	}

	newData, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(historyFilePath, newData, 0644)
}

func getChatHistoryCount() (int, error) {
	historyMutex.Lock()
	defer historyMutex.Unlock()

	data, err := os.ReadFile(historyFilePath)
	if err != nil {
		return 0, err
	}

	var history []ChatMessage
	if err := json.Unmarshal(data, &history); err != nil {
		return 0, err
	}

	return len(history), nil
}
