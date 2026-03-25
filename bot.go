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
    botToken string
    mcpURL   string
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

    log.Println("Bot started in polling mode")
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
    text := update.Message.Text

    var response string
    switch text {
    case "/start":
        response = "Привет! Я бот-шутник 🤖\n\nНапиши /joke или просто 'шутку', чтобы получить шутку о программировании!"
    case "/joke", "шутка", "дай шутку", "joke":
        joke, err := callMCP("joke", nil)
        if err != nil {
            response = "Ошибка: " + err.Error()
        } else {
            response = joke
        }
    default:
        response = "Не понял команду 🤔\n\nДоступные команды:\n/joke - получить шутку\n/start - приветствие"
    }
    sendMessage(chatID, response)
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