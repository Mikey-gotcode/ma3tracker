package routes

import (
	"ma3_tracker/internal/controllers"
	"ma3_tracker/internal/middleware"
	"github.com/gin-gonic/gin"
)

func VehicleRoutes (r *gin.Engine){
	vehicle := r.Group("/vehicle")
	vehicle.Use(middleware.RequireAuth())
	{
		vehicle.POST("/",controllers.CreateVehicle)
	}
}