package controller

import (
	"bytes"
	"encoding/json"
	"l4d2-manager-next/logic"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestServerStartupAdminBoundaries(t *testing.T) {
	oldGet := getServerStartupConfig
	oldUpdate := updateServerStartupConfig
	t.Cleanup(func() {
		getServerStartupConfig = oldGet
		updateServerStartupConfig = oldUpdate
	})

	getServerStartupConfig = func() (logic.ServerStartupConfig, error) {
		return logic.ServerStartupConfig{
			Available: true,
			ExecStart: "/l4d2/srcds_run -game left4dead2 +hostport 27015",
			Port:      27015,
		}, nil
	}
	updateServerStartupConfig = func(request logic.UpdateServerStartupRequest) (logic.ServerStartupConfig, error) {
		return logic.ServerStartupConfig{
			Available:        true,
			WorkingDirectory: request.WorkingDirectory,
			ExecStart:        request.ExecStart,
			Port:             27016,
		}, nil
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/get", func(c *gin.Context) {
		c.Set("role", "guest")
		GetServerStartup(c)
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/get", nil))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("guest get status = %d, want %d", recorder.Code, http.StatusForbidden)
	}

	router = gin.New()
	router.POST("/update", func(c *gin.Context) {
		c.Set("role", "admin")
		UpdateServerStartup(c)
	})
	body := []byte(`{"working_directory":"/l4d2","exec_start":"/l4d2/srcds_run -game left4dead2 +hostport 27016"}`)
	request := httptest.NewRequest(http.MethodPost, "/update", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("admin update status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
	var response logic.ServerStartupConfig
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Port != 27016 || response.WorkingDirectory != "/l4d2" {
		t.Fatalf("unexpected update response: %+v", response)
	}
}
