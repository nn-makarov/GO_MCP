package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
)

// MCP-сервер: отдаёт список инструментов и выполняет их вызовы.
// Бот берёт отсюда описания инструментов, передаёт их модели Groq,
// а когда модель решает вызвать инструмент — бот стучится сюда в /call.

var jokes = []string{
	"Почему программисты не любят природу? Там слишком много багов.",
	"Сколько программистов нужно, чтобы заменить лампочку? Ни одного, это аппаратная проблема.",
	"Почему Go разрабатывали в Google? Потому что в Facebook уже был PHP.",
	"Что сказал один указатель другому? За мной!",
	"Почему гоферы любят Go? Потому что он компилируется быстрее, чем они бегают.",
	"В чём разница между Java и Go? Java-разработчик думает о паттернах, Go-разработчик — о том, как быстрее скомпилировать.",
}

// Tool описывает инструмент так, чтобы модель поняла, когда и как его вызвать.
// Parameters — это JSON Schema, тот же формат, что использует Groq function calling.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

func tools() []Tool {
	return []Tool{
		{
			Name:        "joke",
			Description: "Рассказать случайную шутку про программистов. Вызывай, когда пользователь просит шутку, анекдот или хочет посмеяться.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "greet",
			Description: "Поприветствовать пользователя по имени. Вызывай, когда пользователь представляется или просит поздороваться.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Имя пользователя",
					},
				},
				"required": []string{"name"},
			},
		},
	}
}

func main() {
	http.HandleFunc("/tools", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tools())
	})

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

		var result map[string]string
		switch request.Tool {
		case "joke":
			result = map[string]string{"result": jokes[rand.Intn(len(jokes))]}
		case "greet":
			name, ok := request.Args["name"].(string)
			if !ok || name == "" {
				name = "друг"
			}
			result = map[string]string{"result": fmt.Sprintf("Привет, %s! Рад тебя видеть.", name)}
		default:
			w.WriteHeader(http.StatusNotFound)
			result = map[string]string{"error": "Неизвестный инструмент: " + request.Tool}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	port := os.Getenv("MCP_PORT")
	if port == "" {
		port = "8081"
	}
	log.Println("MCP-сервер слушает на :" + port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
