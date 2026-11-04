package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const defaultBracketDaemonURL = "http://127.0.0.1:7947"

func bracketDaemonBaseURL() string {
	if u := strings.TrimSpace(os.Getenv("BBGO_BRACKET_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	return defaultBracketDaemonURL
}

func (s *Server) registerBracketRoutes(r *gin.Engine) {
	r.GET("/api/brackets", s.proxyBracketList)
	r.POST("/api/brackets", s.proxyBracketAdd)
	r.GET("/api/brackets/:id", s.proxyBracketGet)
	r.DELETE("/api/brackets/:id", s.proxyBracketCancel)
	r.GET("/api/brackets-health", s.proxyBracketHealth)
}

func (s *Server) proxyBracketHealth(c *gin.Context) {
	s.proxyBracket(c, http.MethodGet, "/healthz", nil)
}

func (s *Server) proxyBracketList(c *gin.Context) {
	s.proxyBracket(c, http.MethodGet, "/v1/brackets", nil)
}

func (s *Server) proxyBracketAdd(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body: " + err.Error()})
		return
	}
	s.proxyBracket(c, http.MethodPost, "/v1/brackets", body)
}

func (s *Server) proxyBracketGet(c *gin.Context) {
	id := c.Param("id")
	s.proxyBracket(c, http.MethodGet, "/v1/brackets/"+id, nil)
}

func (s *Server) proxyBracketCancel(c *gin.Context) {
	id := c.Param("id")
	s.proxyBracket(c, http.MethodDelete, "/v1/brackets/"+id, nil)
}

func (s *Server) proxyBracket(c *gin.Context, method, path string, body []byte) {
	url := bracketDaemonBaseURL() + path
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), method, url, reader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   fmt.Sprintf("bracket daemon unreachable at %s: %v", bracketDaemonBaseURL(), err),
			"hint":    "systemctl status bbgo-bracket",
			"daemon":  bracketDaemonBaseURL(),
		})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "read daemon response: " + err.Error()})
		return
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json"
	}
	c.Data(resp.StatusCode, ct, respBody)
}
