package main

import (
    "encoding/json"
    "fmt"
    "log"
    "math/rand"
    "net/http"
    "time"
)

var jokes = []string{
    "Почему программисты не любят природу? Там слишком много багов.",
    "Сколько программистов нужно, чтобы заменить лампочку? Ни одного, это аппаратная проблема.",
    "Почему Go разрабатывали в Google? Потому что в Facebook уже был PHP.",
    "Что сказал один указатель другому? За мной!",
    "Почему гоферы любят Go? Потому что он компилируется быстрее, чем они бегают.",
    "В чем разница между Java и Go? Java-разработчик думает о паттернах, Go-разработчик — о том, как быстрее скомпилировать.",
}

func main() {
    rand.Seed(time.Now().UnixNano())

    // Список инструментов
    http.HandleFunc("/tools", func(w http.ResponseWriter, r *http.Request) {
        tools := []map[string]string{
            {"name": "joke", "description": "Get a random programming joke"},
            {"name": "greet", "description": "Greet someone"},
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(tools)
    })

    // Вызов инструментов
    http.HandleFunc("/call", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            http.Error(w, "Use POST", http.StatusMethodNotAllowed)
            return
        }

        var request struct {
            Tool string                 `json:"tool"`
            Args map[string]interface{} `json:"args"`
        }

        if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
            http.Error(w, "Bad request", http.StatusBadRequest)
            return
        }

        var result interface{}

        switch request.Tool {
        case "joke":
            randomJoke := jokes[rand.Intn(len(jokes))]
            result = map[string]string{"joke": randomJoke}

        case "greet":
            name, ok := request.Args["name"].(string)
            if !ok {
                name = "Bro"
            }
            result = map[string]string{"message": fmt.Sprintf("Hello, %s! Want a joke? Call /call with tool=joke", name)}

        default:
            result = map[string]string{"error": "Unknown tool"}
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(result)
    })

    log.Println("MCP Server with jokes on :8081")
    log.Fatal(http.ListenAndServe(":8081", nil))
}