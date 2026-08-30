package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

// Telegram-бот на Groq с function calling через MCP-сервер.
//
// Схема работы на каждое сообщение:
//   1. Бот берёт список инструментов у MCP-сервера (/tools).
//   2. Отдаёт сообщение пользователя + инструменты модели Groq.
//   3. Если модель решила вызвать инструмент — бот идёт на MCP (/call),
//      получает результат и передаёт его обратно модели для финального ответа.
//   4. Если инструмент не нужен — модель отвечает текстом напрямую.

var (
	botToken   string
	groqAPIKey string
	mcpURL     string
	httpClient = &http.Client{Timeout: 30 * time.Second}
)

func main() {
	botToken = mustEnv("TELEGRAM_BOT_TOKEN")
	groqAPIKey = mustEnv("GROQ_API_KEY")

	mcpURL = os.Getenv("MCP_URL")
	if mcpURL == "" {
		mcpURL = "http://mcp-server:8081"
	}

	// Health-check для хостинга (Render и т.п. пингуют корень).
	go func() {
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		})
		port := os.Getenv("HEALTH_PORT")
		if port == "" {
			port = "10000"
		}
		log.Println("Health-check на :" + port)
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			log.Println("Health-check error:", err)
		}
	}()

	log.Println("Бот запущен (Groq llama-3.3-70b-versatile + MCP function calling)")
	offset := 0
	for {
		for _, update := range getUpdates(offset) {
			if update.Message.Text != "" {
				go handleUpdate(update)
			}
			offset = update.UpdateID + 1
		}
		time.Sleep(time.Second)
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("%s not set", key)
	}
	return v
}

// ---------- Telegram ----------

type Update struct {
	UpdateID int `json:"update_id"`
	Message  struct {
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
}

func getUpdates(offset int) []Update {
	url := "https://api.telegram.org/bot" + botToken +
		"/getUpdates?timeout=30&offset=" + strconv.Itoa(offset)
	resp, err := httpClient.Get(url)
	if err != nil {
		log.Println("getUpdates:", err)
		return nil
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool     `json:"ok"`
		Result []Update `json:"result"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil || !result.OK {
		log.Printf("Telegram API: %s", body)
		return nil
	}
	return result.Result
}

func sendMessage(chatID int64, text string) {
	url := "https://api.telegram.org/bot" + botToken + "/sendMessage"
	body, _ := json.Marshal(map[string]interface{}{"chat_id": chatID, "text": text})
	resp, err := httpClient.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		log.Println("sendMessage:", err)
		return
	}
	resp.Body.Close()
}

func handleUpdate(update Update) {
	chatID := update.Message.Chat.ID
	answer, err := askGroq(update.Message.Text)
	if err != nil {
		log.Println("askGroq:", err)
		sendMessage(chatID, "Извините, произошла ошибка. Попробуйте позже.")
		return
	}
	sendMessage(chatID, answer)
}

// ---------- MCP-сервер ----------

type mcpTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

func fetchTools() ([]mcpTool, error) {
	resp, err := httpClient.Get(mcpURL + "/tools")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var tools []mcpTool
	if err := json.NewDecoder(resp.Body).Decode(&tools); err != nil {
		return nil, err
	}
	return tools, nil
}

func callTool(name string, args map[string]interface{}) string {
	body, _ := json.Marshal(map[string]interface{}{"tool": name, "args": args})
	resp, err := httpClient.Post(mcpURL+"/call", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return "инструмент недоступен"
	}
	defer resp.Body.Close()

	var out struct {
		Result string `json:"result"`
		Error  string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	if out.Error != "" {
		return out.Error
	}
	return out.Result
}

// ---------- Groq (типизированный клиент) ----------

type groqMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []groqToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type groqToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type groqResponse struct {
	Choices []struct {
		Message groqMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// toolsForGroq превращает инструменты MCP в формат function calling Groq.
func toolsForGroq(tools []mcpTool) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Parameters,
			},
		})
	}
	return out
}

func groqRequest(messages []groqMessage, tools []map[string]interface{}) (*groqResponse, error) {
	payload := map[string]interface{}{
		"model":    "llama-3.3-70b-versatile",
		"messages": messages,
	}
	if len(tools) > 0 {
		payload["tools"] = tools
		payload["tool_choice"] = "auto"
	}

	jsonBody, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "https://api.groq.com/openai/v1/chat/completions", bytes.NewBuffer(jsonBody))
	req.Header.Set("Authorization", "Bearer "+groqAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var gr groqResponse
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return nil, err
	}
	if gr.Error != nil {
		return nil, fmt.Errorf("groq: %s", gr.Error.Message)
	}
	if len(gr.Choices) == 0 {
		return nil, fmt.Errorf("groq вернул пустой ответ")
	}
	return &gr, nil
}

// askGroq ведёт диалог с моделью, при необходимости вызывая инструменты MCP.
func askGroq(userMessage string) (string, error) {
	tools, err := fetchTools()
	if err != nil {
		// MCP недоступен — работаем как обычный чат-бот, без инструментов.
		log.Println("MCP недоступен, отвечаю без инструментов:", err)
		tools = nil
	}

	messages := []groqMessage{
		{Role: "system", Content: "Ты дружелюбный помощник. Отвечай кратко и по-русски. " +
			"Если для ответа подходит инструмент — вызови его."},
		{Role: "user", Content: userMessage},
	}

	// Первый запрос: модель либо отвечает текстом, либо просит вызвать инструмент.
	resp, err := groqRequest(messages, toolsForGroq(tools))
	if err != nil {
		return "", err
	}
	msg := resp.Choices[0].Message

	// Инструмент не нужен — возвращаем текст.
	if len(msg.ToolCalls) == 0 {
		return fallback(msg.Content), nil
	}

	// Модель попросила инструменты — выполняем каждый через MCP и
	// докладываем результаты обратно модели.
	messages = append(messages, msg)
	for _, tc := range msg.ToolCalls {
		var args map[string]interface{}
		json.Unmarshal([]byte(tc.Function.Arguments), &args)
		result := callTool(tc.Function.Name, args)

		messages = append(messages, groqMessage{
			Role:       "tool",
			ToolCallID: tc.ID,
			Content:    result,
		})
	}

	// Второй запрос: модель формулирует финальный ответ на основе результатов.
	final, err := groqRequest(messages, nil)
	if err != nil {
		return "", err
	}
	return fallback(final.Choices[0].Message.Content), nil
}

func fallback(s string) string {
	if s == "" {
		return "Не удалось получить ответ."
	}
	return s
}
