package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
)

type LiveMatch struct {
	ID         string `json:"ID"`
	Name       string `json:"NA"`
	Country    string `json:"CT"`
	League     string `json:"CC"`
	Team1      string `json:"T1"`
	Team2      string `json:"T2"`
	Score      string `json:"SS"`
	Minute     string `json:"TM"`
	UpdateTime string `json:"TU"`
	Type       string `json:"type"`

	Source     string `json:"source"`
	UpdatedAt  string `json:"updated_at"`
	MatchTitle string `json:"match_title"`
}

var clients = make(map[*websocket.Conn]bool)
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	// Загрузка .env
	err := godotenv.Load()
	if err != nil {
		log.Println("⚠️ .env не найден, используются системные переменные")
	}

	go pollBookiesAPI()

	router := gin.Default()
	router.GET("/ws", handleWS)

	log.Println("🚀 Сервер слушает порт 8083")
	if err := router.Run(":8083"); err != nil {
		log.Fatal("❌ Ошибка запуска:", err)
	}
}

func handleWS(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Println("❌ WebSocket upgrade error:", err)
		return
	}
	defer conn.Close()

	clients[conn] = true
	log.Println("🔌 WebSocket клиент подключён:", conn.RemoteAddr())

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			log.Println("❌ Клиент отключён:", err)
			delete(clients, conn)
			break
		}
	}
}

func pollBookiesAPI() {
	url := os.Getenv("BOOKIES_API_URL")
	if url == "" {
		log.Fatal("❌ BOOKIES_API_URL не задан в .env")
	}

	interval, _ := strconv.Atoi(os.Getenv("POLL_INTERVAL_SECONDS"))
	if interval == 0 {
		interval = 5
	}

	for {
		time.Sleep(time.Duration(interval) * time.Second)

		resp, err := http.Get(url)
		if err != nil {
			log.Printf("❌ Ошибка запроса к API: %v", err)
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			log.Printf("❌ Ошибка чтения тела ответа: %v", err)
			continue
		}

		var raw map[string]interface{}
		if err := json.Unmarshal(body, &raw); err != nil {
			log.Printf("❌ Ошибка разбора JSON: %v", err)
			continue
		}

		success, ok := raw["success"].(float64)
		if !ok || success != 1 {
			log.Println("⚠️ API вернул success != 1")
			continue
		}

		results, ok := raw["results"].([]interface{})
		if !ok || len(results) == 0 {
			log.Println("⚠️ Пустые results")
			continue
		}

		events, ok := results[0].([]interface{})
		if !ok {
			log.Println("⚠️ Неверный формат results[0]")
			continue
		}

		matches := make([]LiveMatch, 0)

		for _, ev := range events {
			evMap, ok := ev.(map[string]interface{})
			if !ok {
				continue
			}
			match, ok := toLiveMatch(evMap)
			if !ok {
				continue
			}
			log.Printf("✅ Добавлен матч: %s [%s]", match.Name, match.ID)
			matches = append(matches, match)
		}

		log.Printf("📡 Отправлено матчей: %d", len(matches))
		broadcastJSON(matches)
	}
}

func broadcastJSON(data interface{}) {
	for conn := range clients {
		err := conn.WriteJSON(data)
		if err != nil {
			log.Printf("❌ Ошибка отправки клиенту: %v", err)
			conn.Close()
			delete(clients, conn)
		}
	}
}

func toLiveMatch(ev map[string]interface{}) (LiveMatch, bool) {
	if t, ok := ev["type"].(string); !ok || t != "EV" {
		return LiveMatch{}, false
	}
	if _, ok := ev["NA"]; !ok {
		return LiveMatch{}, false
	}

	match := LiveMatch{
		ID:         getString(ev, "ID"),
		Name:       getString(ev, "NA"),
		Country:    getString(ev, "CT"),
		League:     getString(ev, "CC"),
		Team1:      getString(ev, "T1"),
		Team2:      getString(ev, "T2"),
		Score:      getString(ev, "SS"),
		Minute:     getString(ev, "TM"),
		UpdateTime: getString(ev, "TU"),
		Type:       getString(ev, "type"),
		Source:     "bookiesapi",
		UpdatedAt:  time.Now().Format("2006-01-02 15:04:05"),
	}

	match.MatchTitle = match.Team1 + " vs " + match.Team2
	return match, true
}

func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}
