package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
)

// MCP-сервер: отдаёт список инструментов и выполняет их вызовы.
// Часть инструментов работает автономно (шутки), часть ходит в реальные
// сервисы — например, в PriceTracker для отслеживания цен товаров.

// Адрес сервиса PriceTracker (в докер-сети — по имени контейнера).
var priceTrackerURL = envOr("PRICETRACKER_URL", "http://pricetracker-app-1:8001")

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

var jokes = []string{
	"Почему программисты не любят природу? Там слишком много багов.",
	"Сколько программистов нужно, чтобы заменить лампочку? Ни одного, это аппаратная проблема.",
	"Почему Go разрабатывали в Google? Потому что в Facebook уже был PHP.",
	"Что сказал один указатель другому? За мной!",
	"Почему гоферы любят Go? Потому что он компилируется быстрее, чем они бегают.",
	"В чём разница между Java и Go? Java-разработчик думает о паттернах, Go-разработчик — о том, как быстрее скомпилировать.",
}

// Tool описывает инструмент в формате, понятном function calling Groq.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

func obj(props map[string]interface{}, required ...string) map[string]interface{} {
	m := map[string]interface{}{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func str(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": desc}
}

func num(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "number", "description": desc}
}

func tools() []Tool {
	return []Tool{
		{
			Name:        "joke",
			Description: "Рассказать случайную шутку про программистов. Вызывай, когда пользователь просит шутку или анекдот.",
			Parameters:  obj(map[string]interface{}{}),
		},
		{
			Name:        "greet",
			Description: "Поприветствовать пользователя по имени. Вызывай, когда пользователь представляется.",
			Parameters:  obj(map[string]interface{}{"name": str("Имя пользователя")}, "name"),
		},
		{
			Name: "track_product",
			Description: "Добавить товар в отслеживание цен в сервисе PriceTracker. " +
				"Вызывай, когда пользователь просит следить за ценой товара и даёт ссылку, название и цену.",
			Parameters: obj(map[string]interface{}{
				"url":   str("Ссылка на товар"),
				"title": str("Название товара"),
				"price": num("Текущая цена в рублях"),
			}, "url", "title", "price"),
		},
		{
			Name: "list_products",
			Description: "Показать список товаров, за ценами которых сейчас идёт слежение в PriceTracker. " +
				"Вызывай, когда пользователь спрашивает, какие товары отслеживаются.",
			Parameters: obj(map[string]interface{}{}),
		},
		{
			Name: "product_stats",
			Description: "Показать текущую цену и статистику по конкретному отслеживаемому товару по его ID. " +
				"Вызывай, когда пользователь спрашивает цену или историю конкретного товара.",
			Parameters: obj(map[string]interface{}{
				"id": num("ID товара в PriceTracker"),
			}, "id"),
		},
		{
			Name: "update_price",
			Description: "Обновить текущую цену отслеживаемого товара по его ID. Новое значение " +
				"добавляется в историю цен. Вызывай, когда пользователь сообщает, что цена товара изменилась.",
			Parameters: obj(map[string]interface{}{
				"id":    num("ID товара в PriceTracker"),
				"price": num("Новая цена в рублях"),
			}, "id", "price"),
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
		var req struct {
			Tool string                 `json:"tool"`
			Args map[string]interface{} `json:"args"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"result": dispatch(req.Tool, req.Args)})
	})

	port := envOr("MCP_PORT", "8081")
	log.Println("MCP-сервер слушает на :" + port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// dispatch выполняет инструмент и возвращает текстовый результат для модели.
func dispatch(tool string, args map[string]interface{}) string {
	switch tool {
	case "joke":
		return jokes[rand.Intn(len(jokes))]

	case "greet":
		name, _ := args["name"].(string)
		if name == "" {
			name = "друг"
		}
		return fmt.Sprintf("Привет, %s! Рад тебя видеть.", name)

	case "track_product":
		url, _ := args["url"].(string)
		title, _ := args["title"].(string)
		price, _ := args["price"].(float64)
		return trackProduct(url, title, price)

	case "list_products":
		return listProducts()

	case "product_stats":
		id, _ := args["id"].(float64)
		return productStats(int(id))

	case "update_price":
		id, _ := args["id"].(float64)
		price, _ := args["price"].(float64)
		return updatePrice(int(id), price)

	default:
		return "Неизвестный инструмент: " + tool
	}
}

// ---------- вызовы PriceTracker ----------

func trackProduct(url, title string, price float64) string {
	body, _ := json.Marshal(map[string]interface{}{
		"url": url, "title": title, "current_price": price,
	})
	resp, err := http.Post(priceTrackerURL+"/api/track", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return "PriceTracker недоступен: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "Не удалось добавить товар (код " + fmt.Sprint(resp.StatusCode) + ")"
	}
	return fmt.Sprintf("Товар «%s» добавлен в отслеживание по цене %.0f ₽.", title, price)
}

func listProducts() string {
	resp, err := http.Get(priceTrackerURL + "/api/tracked-products")
	if err != nil {
		return "PriceTracker недоступен: " + err.Error()
	}
	defer resp.Body.Close()

	var products []struct {
		ID           int     `json:"id"`
		Title        string  `json:"title"`
		CurrentPrice float64 `json:"current_price"`
	}
	data, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(data, &products); err != nil {
		return "Не удалось прочитать список товаров"
	}
	if len(products) == 0 {
		return "Список отслеживаемых товаров пуст."
	}
	out := "Отслеживаемые товары:\n"
	for _, p := range products {
		out += fmt.Sprintf("• [%d] %s — %.0f ₽\n", p.ID, p.Title, p.CurrentPrice)
	}
	return out
}

func productStats(id int) string {
	resp, err := http.Get(fmt.Sprintf("%s/api/stats/%d", priceTrackerURL, id))
	if err != nil {
		return "PriceTracker недоступен: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Sprintf("Товар с ID %d не найден.", id)
	}

	var stats struct {
		Product struct {
			Title        string  `json:"title"`
			CurrentPrice float64 `json:"current_price"`
		} `json:"product"`
		Stats struct {
			TotalRecords int     `json:"total_records"`
			PriceChange  float64 `json:"price_change"`
		} `json:"stats"`
	}
	data, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(data, &stats); err != nil {
		return "Не удалось прочитать статистику"
	}
	return fmt.Sprintf("«%s»: текущая цена %.0f ₽, записей в истории: %d, изменение цены: %+.0f ₽.",
		stats.Product.Title, stats.Product.CurrentPrice, stats.Stats.TotalRecords, stats.Stats.PriceChange)
}

func updatePrice(id int, price float64) string {
	body, _ := json.Marshal(map[string]interface{}{"price": price})
	req, _ := http.NewRequest("PATCH", fmt.Sprintf("%s/api/products/%d/price", priceTrackerURL, id), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "PriceTracker недоступен: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Sprintf("Товар с ID %d не найден.", id)
	}
	if resp.StatusCode != http.StatusOK {
		return "Не удалось обновить цену (код " + fmt.Sprint(resp.StatusCode) + ")"
	}

	var product struct {
		Title string `json:"title"`
	}
	data, _ := io.ReadAll(resp.Body)
	json.Unmarshal(data, &product)
	return fmt.Sprintf("Цена товара «%s» обновлена на %.0f ₽.", product.Title, price)
}
