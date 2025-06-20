package main

import (
	"log"
	"net/http"

	"ma3_tracker/internal/config"
	"ma3_tracker/internal/logger"
	"ma3_tracker/internal/middleware"
	"ma3_tracker/internal/routes"

	"github.com/gin-gonic/gin"
//ginlog "github.com/gin-contrib/logger"
)

func main() {
	// Initialize structured logging to file
	logger.Setup()

	// Connect to the database
	config.InitDB()

	// Setup Gin router
	r := routes.SetupRouter()

	// Recovery middleware
	r.Use(gin.Recovery())

	    // Request logging middleware
   // r.Use(ginlog.SetLogger())

    // Wrap with CORS
	handler := middleware.EnableCORS(r)

	log.Println("🚀 Server running at :8080")
	log.Fatal(http.ListenAndServe("0.0.0.0:8080", handler))
}
