package controller

import (
	"l4d2-manager-next/logic"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

var collectServerDiagnostics = logic.CollectServerDiagnostics

func GetServerDiagnostics(c *gin.Context) {
	if !requireServerStartupAdmin(c) {
		return
	}
	c.JSON(http.StatusOK, collectServerDiagnostics(3*time.Second))
}
