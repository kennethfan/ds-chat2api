package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func HandleStatus(c *gin.Context, status StatusProvider) {
	connected := "disconnected"
	if status.Connected() {
		connected = "connected"
	}
	c.JSON(http.StatusOK, gin.H{"status": connected})
}
