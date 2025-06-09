package main

import (
	"log"
	"net/http"


	"ma3_tracker/internal/config"
	"ma3_tracker/internal/middleware"
	"ma3_tracker/internal/routes"
	

	//"ma3_tracker/internal/controllers"
	//"ma3_tracker/internal/config"
)




func main() {
	// Connect to the database
	config.InitDB()

	// Setup Gin router
	r := routes.SetupRouter()

	// Wrap router with CORS middleware (custom)
	handler := middleware.EnableCORS(r)

	log.Println("🚀 Server running at :8080")
	log.Fatal(http.ListenAndServe("0.0.0.0:8080", handler))
}
