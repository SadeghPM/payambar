package main

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/4xmen/payambar/internal/auth"
	"github.com/4xmen/payambar/internal/db"
	"github.com/4xmen/payambar/internal/handlers"
	"github.com/4xmen/payambar/internal/ws"
	"github.com/4xmen/payambar/pkg/config"
	"github.com/gin-gonic/gin"
	"github.com/ulule/limiter/v3"
	"github.com/ulule/limiter/v3/drivers/store/memory"
)

//go:embed static/*
var staticFS embed.FS

func rateLimitMiddleware(limiterInstance *limiter.Limiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		limiterContext, err := limiterInstance.Get(c.Request.Context(), c.ClientIP())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": __("rate limiter error")})
			c.Abort()
			return
		}

		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", limiterContext.Limit))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", limiterContext.Remaining))
		c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", limiterContext.Reset))

		if limiterContext.Reached {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": __("rate limit exceeded")})
			c.Abort()
			return
		}

		c.Next()
	}
}

type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w responseBodyWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w responseBodyWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

func serverErrorLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		blw := &responseBodyWriter{body: bytes.NewBuffer(nil), ResponseWriter: c.Writer}
		c.Writer = blw

		c.Next()

		if c.Writer.Status() >= http.StatusInternalServerError {
			log.Printf(
				"HTTP %d %s %s ip=%s duration=%s errors=%q response=%q",
				c.Writer.Status(),
				c.Request.Method,
				c.Request.URL.Path,
				c.ClientIP(),
				time.Since(start).Truncate(time.Millisecond),
				c.Errors.ByType(gin.ErrorTypeAny).String(),
				strings.TrimSpace(blw.body.String()),
			)
		}
	}
}

func panicRecovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		log.Printf(
			"panic recovered method=%s path=%s ip=%s error=%v\n%s",
			c.Request.Method,
			c.Request.URL.Path,
			c.ClientIP(),
			recovered,
			debug.Stack(),
		)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": __("internal server error")})
	})
}

func shouldServeSPA(c *gin.Context) bool {
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		return false
	}

	accept := c.GetHeader("Accept")
	if !strings.Contains(accept, "text/html") {
		return false
	}

	reqPath := c.Request.URL.Path
	if reqPath == "" {
		return false
	}

	// Do not SPA-fallback unknown file-like paths (common scanner probes).
	if ext := strings.ToLower(path.Ext(reqPath)); ext != "" {
		return false
	}

	return true
}

func main() {
	cfg := config.Load()

	if len(os.Args) > 1 {
		if err := runCommand(cfg, os.Args[1:]); err != nil {
			log.Fatalf("%v", err)
		}
		return
	}

	if err := runServer(cfg); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func runCommand(cfg *config.Config, args []string) error {
	command := args[0]

	switch command {
	case "status":
		return runStatus(cfg, os.Stdout, args[1:])
	case "-h", "--help", "help":
		printUsage(os.Stdout)
		return nil
	default:
		printUsage(os.Stderr)
		return fmt.Errorf("unknown command: %s", command)
	}
}

func printUsage(out *os.File) {
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  payambar           Start the web server")
	fmt.Fprintln(out, "  payambar status    Show application statistics")
	fmt.Fprintln(out, "  payambar status --json")
}

func runServer(cfg *config.Config) error {
	// Ensure data directories exist
	os.MkdirAll(cfg.FileStoragePath, 0755)
	os.MkdirAll("/data", 0755)

	// Initialize database
	database, err := db.New(cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer database.Close()

	// Initialize services
	authSvc := auth.New(database.GetConn(), cfg.JWTSecret)

	// Initialize WebSocket hub
	hub := ws.NewHub(database.GetConn())
	go hub.Run()

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(authSvc)
	msgHandler := handlers.NewMessageHandler(database.GetConn(), hub, cfg.FileStoragePath, cfg.MaxUploadSize, cfg.StunServers, cfg.TurnServer, cfg.TurnUsername, cfg.TurnPassword)

	// Setup router
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(serverErrorLogger())
	router.Use(gin.Logger())
	router.Use(panicRecovery())
	router.MaxMultipartMemory = cfg.MaxUploadSize

	// CORS middleware
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", cfg.CORSOrigins)
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// Public endpoints
	api := router.Group("/api")
	{
		loginLimiter := limiter.New(memory.NewStore(), limiter.Rate{Period: time.Minute, Limit: 5})
		registerLimiter := limiter.New(memory.NewStore(), limiter.Rate{Period: time.Minute, Limit: 2})

		// Auth endpoints
		api.POST("/auth/register", rateLimitMiddleware(registerLimiter), authHandler.Register)
		api.POST("/auth/login", rateLimitMiddleware(loginLimiter), authHandler.Login)

		// Public profile endpoint
		api.GET("/users/:username", msgHandler.GetUserProfile)
	}

	// Protected endpoints
	protected := api.Group("")
	protected.Use(authHandler.AuthMiddleware())
	{
		// Messages
		protected.GET("/messages", msgHandler.GetConversation)
		protected.GET("/conversations", msgHandler.GetConversations)
		protected.GET("/users", msgHandler.GetUsers)
		protected.POST("/conversations", msgHandler.CreateConversation)
		protected.DELETE("/conversations/:id", msgHandler.DeleteConversation)
		protected.PUT("/messages/:id/delivered", msgHandler.MarkAsDelivered)
		protected.PUT("/messages/:id/read", msgHandler.MarkAsRead)
		protected.DELETE("/messages/:id", msgHandler.DeleteMessage)
		protected.POST("/upload", msgHandler.UploadFile)

		// Profile
		protected.GET("/profile", msgHandler.GetMyProfile)
		protected.PUT("/profile", msgHandler.UpdateProfile)
		protected.POST("/profile/avatar", msgHandler.UploadAvatar)
		protected.DELETE("/profile", msgHandler.DeleteAccount)

		// WebRTC
		protected.GET("/webrtc/config", msgHandler.GetWebRTCConfig)
	}

	// Serve uploaded files from configured storage path
	router.Static("/api/files", cfg.FileStoragePath)

	// WebSocket endpoint
	router.GET("/ws", authHandler.AuthMiddleware(), hub.HandleWebSocket)

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// Serve embedded static files
	staticAssets, err := fs.Sub(staticFS, "static")
	if err == nil {
		// Serve manifest.json
		router.GET("/manifest.json", func(c *gin.Context) {
			data, err := fs.ReadFile(staticFS, "static/manifest.json")
			if err != nil {
				c.JSON(404, gin.H{"error": __("not found")})
				return
			}
			c.Header("Cache-Control", "public, max-age=3600")
			c.Data(http.StatusOK, "application/json", data)
		})

		// Serve service worker
		router.GET("/sw.js", func(c *gin.Context) {
			data, err := fs.ReadFile(staticFS, "static/sw.js")
			if err != nil {
				c.JSON(404, gin.H{"error": __("not found")})
				return
			}
			c.Header("Cache-Control", "public, max-age=3600")
			c.Data(http.StatusOK, "application/javascript", data)
		})

		// Serve static files (CSS, JS with cache)
		router.GET("/styles.css", func(c *gin.Context) {
			data, err := fs.ReadFile(staticFS, "static/styles.css")
			if err != nil {
				c.JSON(404, gin.H{"error": __("not found")})
				return
			}
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
			c.Data(http.StatusOK, "text/css; charset=utf-8", data)
		})

		router.GET("/app.js", func(c *gin.Context) {
			data, err := fs.ReadFile(staticFS, "static/app.js")
			if err != nil {
				c.JSON(404, gin.H{"error": __("not found")})
				return
			}
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
			c.Data(http.StatusOK, "application/javascript; charset=utf-8", data)
		})

		router.GET("/vue.global.prod.js", func(c *gin.Context) {
			data, err := fs.ReadFile(staticFS, "static/vue.global.prod.js")
			if err != nil {
				c.JSON(404, gin.H{"error": __("not found")})
				return
			}
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
			c.Data(http.StatusOK, "application/javascript; charset=utf-8", data)
		})

		// Serve fonts
		router.GET("/fonts/*filepath", func(c *gin.Context) {
			file := strings.TrimPrefix(c.Param("filepath"), "/")
			data, err := fs.ReadFile(staticFS, path.Join("static/fonts", file))
			if err != nil {
				c.JSON(404, gin.H{"error": __("not found")})
				return
			}
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
			c.Data(http.StatusOK, "font/woff2", data)
		})

		// Serve PWA icons
		serveIcon := func(filename string, mimeType string) gin.HandlerFunc {
			return func(c *gin.Context) {
				data, err := fs.ReadFile(staticFS, "static/"+filename)
				if err != nil {
					c.JSON(404, gin.H{"error": __("not found")})
					return
				}
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
				c.Data(http.StatusOK, mimeType, data)
			}
		}
		router.GET("/favicon.svg", serveIcon("favicon.svg", "image/svg+xml"))
		router.GET("/favicon-96.png", serveIcon("favicon-96.png", "image/png"))
		router.GET("/favicon-192.png", serveIcon("favicon-192.png", "image/png"))
		router.GET("/favicon-512.png", serveIcon("favicon-512.png", "image/png"))
		router.GET("/favicon-maskable-192.png", serveIcon("favicon-maskable-192.png", "image/png"))
		router.GET("/favicon-maskable-512.png", serveIcon("favicon-maskable-512.png", "image/png"))
		router.GET("/apple-touch-icon.png", serveIcon("apple-touch-icon.png", "image/png"))
		router.GET("/screenshot-540.png", serveIcon("screenshot-540.png", "image/png"))
		router.GET("/screenshot-1280.png", serveIcon("screenshot-1280.png", "image/png"))

		// Serve index.html for all other routes (SPA)
		router.NoRoute(func(c *gin.Context) {
			if !shouldServeSPA(c) {
				c.JSON(http.StatusNotFound, gin.H{"error": __("not found")})
				return
			}

			data, err := fs.ReadFile(staticFS, "static/index.html")
			if err != nil {
				c.JSON(404, gin.H{"error": __("not found")})
				return
			}
			c.Header("Cache-Control", "public, max-age=3600")
			c.Data(http.StatusOK, "text/html; charset=utf-8", data)
		})

		// Ensure staticAssets is used (required by Go compiler)
		_ = staticAssets
	} else {
		// Fallback if embed fails
		log.Printf("Warning: Could not embed static files: %v", err)
		router.NoRoute(func(c *gin.Context) {
			c.JSON(404, gin.H{"error": __("not found")})
		})
	}

	// Start server
	addr := fmt.Sprintf("0.0.0.0:%s", cfg.Port)
	log.Printf("Starting server on %s", addr)

	// Setup graceful shutdown
	sigint := make(chan os.Signal, 1)
	signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigint
		log.Println("\nShutting down gracefully...")
		os.Exit(0)
	}()

	if err := router.Run(addr); err != nil {
		return err
	}

	return nil
}
