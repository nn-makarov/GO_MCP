package main

import (
    "bytes"
    "encoding/json"
    "io"
    "log"
    "net/http"
    "os"
    "time"
)

var (
    botToken   string
    groqAPIKey string
)

func main() {
    botToken = os.Getenv("TELEGRAM_BOT_TOKEN")
    if botToken == "" {
        log.Fatal("TELEGRAM_BOT_TOKEN not set")
    }

    groqAPIKey = os.Getenv("GROQ_API_KEY")
    if groqAPIKey == "" {
        log.Fatal("GROQ_API_KEY not set")
    }

    // Health check server for Render
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

    log.Println("AI Bot started (Groq with llama-3.3-70b-versatile)")
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
    url := "https://api.telegram.org/bot" + botToken + "/getUpdates?timeout=30&offset=" + itoa(offset)
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
        log.Printf("Telegram API error: %s", body)
        return nil
    }
    return result.Result
}

func handleUpdate(update Update) {
    chatID := update.Message.Chat.ID
    userText := update.Message.Text

    response, err := callGroq(userText)
    if err != nil {
        log.Println("Groq error:", err)
        sendMessage(chatID, "Извините, произошла ошибка. Попробуйте позже.")
        return
    }

    sendMessage(chatID, response)
}

func callGroq(userMessage string) (string, error) {
    url := "https://api.groq.com/openai/v1/chat/completions"

    messages := []map[string]interface{}{
        {"role": "system", "content": "You are a friendly assistant. Answer briefly and in Russian."},
        {"role": "user", "content": userMessage},
    }

    body := map[string]interface{}{
        "model":    "llama-3.3-70b-versatile",
        "messages": messages,
    }

    jsonBody, _ := json.Marshal(body)
    req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
    req.Header.Set("Authorization", "Bearer "+groqAPIKey)
    req.Header.Set("Content-Type", "application/json")

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    var result map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&result)

    choices, ok := result["choices"].([]interface{})
    if !ok || len(choices) == 0 {
        return "Не удалось получить ответ", nil
    }

    choice := choices[0].(map[string]interface{})
    message := choice["message"].(map[string]interface{})
    content, _ := message["content"].(string)

    if content == "" {
        return "Не удалось получить ответ", nil
    }
    return content, nil
}

func sendMessage(chatID int64, text string) {
    url := "https://api.telegram.org/bot" + botToken + "/sendMessage"
    body := map[string]interface{}{
        "chat_id": chatID,
        "text":    text,
    }
    jsonBody, _ := json.Marshal(body)
    http.Post(url, "application/json", bytes.NewBuffer(jsonBody))
}

func itoa(n int) string {
    if n == 0 {
        return "0"
    }
    var digits []byte
    neg := false
    if n < 0 {
        neg = true
        n = -n
    }
    for n > 0 {
        digits = append([]byte{byte('0' + n%10)}, digits...)
        n /= 10
    }
    if neg {
        digits = append([]byte{'-'}, digits...)
    }
    return string(digits)
}