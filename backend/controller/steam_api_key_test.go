package controller

import (
	"l4d2-manager-next/middlewares"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSteamAPIKeySettingRejectsNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/steam-api-key", func(c *gin.Context) {
		c.Set("role", middlewares.RoleGuest)
	}, GetSteamAPIKeySetting)

	request := httptest.NewRequest(http.MethodPost, "/steam-api-key", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", response.Code)
	}
}
