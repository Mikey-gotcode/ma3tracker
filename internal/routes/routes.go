package routes

import (
	"github.com/gin-gonic/gin"
)

func SetupRouter() *gin.Engine{
	r:=gin.Default()

	// Auth routes
	AuthRoutes(r)
	DriverRoutes(r)
	SaccoRoutes(r)
	VehicleRoutes(r)
	AdminRoutes(r)
	WebSocketRoutes(r)
	CommuterRoutes(r)

	r.Run(":8080")

	return r
}