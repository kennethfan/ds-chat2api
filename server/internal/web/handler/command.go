package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

func HandleCommand(c *gin.Context, sender CommandSender) {
	var req struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if req.Method == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "method is required"})
		return
	}

	resp, err := sender.SendRequest(req.Method, req.Params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}
