package web

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"ds-chat2api/server/internal/web/handler"
)

type HttpServer struct {
	sender handler.CommandSender
	status handler.StatusProvider
	apiKey string
}

func NewHttpServer(sender handler.CommandSender, status handler.StatusProvider, apiKey string) *HttpServer {
	return &HttpServer{sender: sender, status: status, apiKey: apiKey}
}

func (s *HttpServer) Start(port string) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	if s.apiKey != "" {
		r.Use(s.authMiddleware())
	}

	r.POST("/api/command", func(c *gin.Context) {
		handler.HandleCommand(c, s.sender)
	})
	r.GET("/api/status", func(c *gin.Context) {
		handler.HandleStatus(c, s.status)
	})
	r.GET("/api/stats", func(c *gin.Context) {
		handler.HandleStats(c, s.status)
	})

	addr := fmt.Sprintf(":%s", port)
	log.Printf("HTTP API listening on http://localhost%s", addr)
	if s.apiKey == "" {
		log.Println("  ⚠ Auth disabled (set API_KEY env to enable)")
	}
	go func() {
		if err := http.ListenAndServe(addr, r.Handler()); err != nil {
			log.Fatal(err)
		}
	}()
}

func (s *HttpServer) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer "+s.apiKey {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}
