package routes

import (
	//"ma3_tracker/internal/controllers"
	"ma3_tracker/internal/middleware"
	"ma3_tracker/internal/controllers"
	"github.com/gin-gonic/gin"
)

func CommuterRoutes (r *gin.Engine){
	commuter :=r.Group("/commuter")
	commuter.Use(middleware.RequireAuthWithRole("commuter"))
	{
		commuter.POST("/routes/find-optimal", controllers.FindOptimalRoute)
		   // Route to get all routes visible to a commuter
        commuter.GET("/routes", controllers.ListAllCommuterRoutes) // Assuming ListRoutes returns all public routes

        // Route to get all vehicles visible to a commuter
        commuter.GET("/vehicles", controllers.ListActiveVehicles) // Assuming ListVehicles returns all public vehicles

        // Route to get all drivers visible to a commuter
        commuter.GET("/drivers", controllers.ListDrivers) // Assuming ListDrivers returns all public drivers

	}

}

