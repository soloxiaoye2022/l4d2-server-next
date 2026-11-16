package controller

import (
	"net/http"

	"l4d2-manager-next/logic"

	"github.com/gin-gonic/gin"
)

// GetPlatform is public on purpose: the frontend needs platform capabilities
// before login so it can hide systemd-only controls on Windows.
func GetPlatform(c *gin.Context) {
	c.JSON(http.StatusOK, logic.GetPlatformCapabilities())
}
