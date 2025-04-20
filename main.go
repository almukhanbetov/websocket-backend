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

var clients = make(map[*websocket.Conn]bool)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// ⚠️ Разрешаем соединение с любого фронта (удобно для dev)
		return true
	},
}

func handleWS(c *gin.Context) {
	log.Println("➡️ WebSocket запрос...")

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Println("❌ Ошибка апгрейда:", err)
		return
	}
	defer conn.Close()

	clients[conn] = true
	log.Println("🔌 Клиент подключён:", conn.RemoteAddr())

	// Держим соединение живым — читаем сообщения
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			log.Println("❌ Клиент отключён:", err)
			delete(clients, conn)
			break
		}
	}
}

func broadcastLoop() {
	for {
		time.Sleep(10 * time.Second)

		message := []map[string]interface{}{
			{
				"type": "EV",
				"NA":   "Тестовый матч",
				"CT":   "Лига отладки",
				"ID":   time.Now().Format("15:04:05"),
			},
		}

		for conn := range clients {
			err := conn.WriteJSON(message)
			if err != nil {
				log.Println("❌ Ошибка отправки:", err)
				conn.Close()
				delete(clients, conn)
			} else {
				log.Printf("📤 Отправлено клиенту: %v", conn.RemoteAddr())
			}
		}
	}
}

func pingLoop() {
	for {
		time.Sleep(15 * time.Second)
		for conn := range clients {
			err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping","ts":"`+time.Now().Format("15:04:05")+`"}`))
			if err != nil {
				log.Println("❌ Ping не прошёл:", err)
				conn.Close()
				delete(clients, conn)
			}
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
			log.Printf("❌ Ошибка чтения ответа API: %v", err)
			continue
		}

		var raw map[string]interface{}
		if err := json.Unmarshal(body, &raw); err != nil {
			log.Printf("❌ Ошибка разбора JSON: %v", err)
			continue
		}

		success, ok := raw["success"].(float64)
		if !ok || success != 1 {
			log.Println("⚠️ Ответ API: success != 1")
			continue
		}

		// 🎯 Разбор событий
		results, ok := raw["results"].([]interface{})
		if !ok || len(results) == 0 {
			log.Println("⚠️ Нет результатов в ответе")
			continue
		}

		events, ok := results[0].([]interface{})
		if !ok {
			log.Println("⚠️ Формат событий не []interface{}")
			continue
		}

		clean := make([]map[string]interface{}, 0)

		for _, ev := range events {
			evMap, ok := ev.(map[string]interface{})
			if !ok {
				continue
			}

			if t, ok := evMap["type"].(string); !ok || t != "EV" {
				continue
			}

			if _, ok := evMap["NA"]; !ok {
				continue
			}

			clean = append(clean, evMap)
			log.Printf("✅ Матч: %s [%s]", evMap["NA"], evMap["ID"])
		}

		log.Printf("📡 Отправлено событий: %d", len(clean))
		broadcastJSON(clean)
	}
}
func broadcastJSON(data interface{}) {
	for conn := range clients {
		err := conn.WriteJSON(data)
		if err != nil {
			log.Println("❌ Ошибка при отправке клиенту:", err)
			conn.Close()
			delete(clients, conn)
		} else {
			log.Printf("📤 Отправлено клиенту: %v", conn.RemoteAddr())
		}
	}
}
func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("⚠️ .env не найден, используются переменные окружения системы")
	}
	go pollBookiesAPI()
	go pingLoop()
	
	router := gin.Default()
	router.GET("/ws", handleWS)

	log.Println("🚀 Сервер слушает порт :8083")
	if err := router.Run(":8083"); err != nil {
		log.Fatal("❌ Ошибка запуска сервера:", err)
	}
}
