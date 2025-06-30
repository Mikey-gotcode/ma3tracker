package routes

import (
	//"ma3_tracker/internal/controllers"
	"ma3_tracker/internal/middleware"
	"ma3_tracker/internal/controllers"
	"github.com/gin-gonic/gin"
)

func DriverRoutes (r *gin.Engine){
	driver := r.Group("/driver")
	driver.Use(middleware.RequireAuthWithRole("driver"))
	{
		 driver.GET("/vehicles/driver/:driverId", controllers.GetVehicleByDriverID)
		 driver.PATCH("/vehicles/:id", controllers.UpdateVehicleStatus)

	}

	
}