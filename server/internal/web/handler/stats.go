package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func HandleStats(c *gin.Context, status StatusProvider) {
	c.JSON(http.StatusOK, status.Stats())
}
