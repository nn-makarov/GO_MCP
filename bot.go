package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "os"
    "time"
)

var (
    botToken    string
    mcpURL      string
    deepseekKey string
)

func main() {
    botToken = os.Getenv("TELEGRAM_BOT_TOKEN")
    if botToken == "" {
        log.Fatal("TELEGRAM_BOT_TOKEN not set")
    }

    mcpURL = os.Getenv("MCP_URL")
    if mcpURL == "" {
        mcpURL = "https://go-mcp.onrender.com/call"
    }

    deepseekKey = os.Getenv("DEEPSEEK_API_KEY")
    if deepseekKey == "" {
        log.Fatal("DEEPSEEK_API_KEY not set")
    }

    // Запускаем health check сервер для Render
    go func() {
        http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
            w.WriteHeader(http.StatusOK)
            w.Write([]byte("OK"))
        })
        log.Println("Health check server listening on :10000")
        if err := http.ListenAndServe(":10000", nil); err != nil {
            log.Println("Health check server error:", err)
        }
    }()

    log.Println("AI Bot started in polling mode")
    offset := 0
    for {
        updates := getUpdates(offset)
        for _, update := range updates {
            handleUpdate(update)
            offset = update.UpdateID + 1
        }
        time.Sleep(1 * time.Second)
    }
}

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
    url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?timeout=30&offset=%d", botToken, offset)
    resp, err := http.Get(url)
    if err != nil {
        log.Println("Error getting updates:", err)
        return nil
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    var result struct {
        OK     bool     `json:"ok"`
        Result []Update `json:"result"`
    }
    json.Unmarshal(body, &result)

    if !result.OK {
        return nil
    }
    return result.Result
}

func handleUpdate(update Update) {
    chatID := update.Message.Chat.ID
    userText := update.Message.Text

    response, err := callDeepSeek(userText)
    if err != nil {
        log.Println("DeepSeek error:", err)
        sendMessage(chatID, "Извините, произошла ошибка. Попробуйте позже.")
        return
    }

    sendMessage(chatID, response)
}

func callDeepSeek(userMessage string) (string, error) {
    url := "https://api.deepseek.com/v1/chat/completions"

    messages := []map[string]interface{}{
        {
            "role":    "system",
            "content": "You are a friendly assistant. You can call tools: get_joke, greet. If user asks for a joke, call get_joke. If user asks to greet someone, call greet with name. Otherwise respond naturally.",
        },
        {
            "role":    "user",
            "content": userMessage,
        },
    }

    tools := []map[string]interface{}{
        {
            "type": "function",
            "function": map[string]interface{}{
                "name":        "get_joke",
                "description": "Get a random programming joke",
                "parameters": map[string]interface{}{
                    "type":       "object",
                    "properties": map[string]interface{}{},
                },
            },
        },
        {
            "type": "function",
            "function": map[string]interface{}{
                "name":        "greet",
                "description": "Greet a person by name",
                "parameters": map[string]interface{}{
                    "type": "object",
                    "properties": map[string]interface{}{
                        "name": map[string]string{"type": "string"},
                    },
                    "required": []string{"name"},
                },
            },
        },
    }

    body := map[string]interface{}{
        "model":       "deepseek-chat",
        "messages":    messages,
        "tools":       tools,
        "tool_choice": "auto",
    }

    jsonBody, _ := json.Marshal(body)
    req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
    req.Header.Set("Authorization", "Bearer "+deepseekKey)
    req.Header.Set("Content-Type", "application/json")

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    var result map[string]interface{}
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return "", err
    }

    choices, ok := result["choices"].([]interface{})
    if !ok || len(choices) == 0 {
        return "Не удалось получить ответ", nil
    }

    choice := choices[0].(map[string]interface{})
    message := choice["message"].(map[string]interface{})

    // Проверяем, есть ли вызов инструмента
    if toolCalls, ok := message["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
        toolCall := toolCalls[0].(map[string]interface{})
        function := toolCall["function"].(map[string]interface{})
        toolName := function["name"].(string)

        var args map[string]interface{}
        if argsRaw, ok := function["arguments"].(string); ok {
            json.Unmarshal([]byte(argsRaw), &args)
        }

        mcpResult, err := callMCP(toolName, args)
        if err != nil {
            return "", err
        }

        return finalizeWithToolResult(userMessage, toolName, mcpResult)
    }

    if content, ok := message["content"].(string); ok {
        return content, nil
    }
    return "Не удалось получить ответ", nil
}

func finalizeWithToolResult(userMessage, toolName, toolResult string) (string, error) {
    url := "https://api.deepseek.com/v1/chat/completions"

    messages := []map[string]interface{}{
        {
            "role":    "system",
            "content": "You are a friendly assistant. Answer naturally based on the tool result.",
        },
        {
            "role":    "user",
            "content": userMessage,
        },
        {
            "role":      "tool",
            "content":   toolResult,
            "tool_call_id": "call_1",
        },
    }

    body := map[string]interface{}{
        "model":    "deepseek-chat",
        "messages": messages,
    }

    jsonBody, _ := json.Marshal(body)
    req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
    req.Header.Set("Authorization", "Bearer "+deepseekKey)
    req.Header.Set("Content-Type", "application/json")

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    var result map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&result)

    choices, _ := result["choices"].([]interface{})
    if len(choices) == 0 {
        return toolResult, nil
    }
    choice := choices[0].(map[string]interface{})
    message := choice["message"].(map[string]interface{})
    content, _ := message["content"].(string)
    if content == "" {
        return toolResult, nil
    }
    return content, nil
}

func callMCP(tool string, args map[string]interface{}) (string, error) {
    reqBody := map[string]interface{}{
        "tool": tool,
        "args": args,
    }
    jsonBody, _ := json.Marshal(reqBody)

    resp, err := http.Post(mcpURL, "application/json", bytes.NewBuffer(jsonBody))
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)

    var result map[string]interface{}
    json.Unmarshal(body, &result)

    if joke, ok := result["joke"].(string); ok {
        return joke, nil
    }
    if msg, ok := result["message"].(string); ok {
        return msg, nil
    }
    if errMsg, ok := result["error"].(string); ok {
        return "", fmt.Errorf(errMsg)
    }

    return string(body), nil
}

func sendMessage(chatID int64, text string) {
    url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
    body := map[string]interface{}{
        "chat_id": chatID,
        "text":    text,
    }
    jsonBody, _ := json.Marshal(body)
    http.Post(url, "application/json", bytes.NewBuffer(jsonBody))
}