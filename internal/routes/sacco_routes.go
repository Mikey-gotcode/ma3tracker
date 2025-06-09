package routes

import (
	"ma3_tracker/internal/controllers"
	"ma3_tracker/internal/middleware"
	"github.com/gin-gonic/gin"
)

func SaccoRoutes (r *gin.Engine){
	sacco :=r.Group("/sacco")
	sacco.Use(middleware.RequireAuthWithRole("sacco"))
	{
		//sacco.POST("/",controllers.CreateSacco)
		sacco.POST("/routes",controllers.CreateRoute)
		sacco.PATCH("/routes/:id/stages", controllers.AddStagesToRoute) // New endpoint for adding/updating stages
        sacco.GET("/routes", controllers.ListRoutes)
		sacco.GET("/routes/:id", controllers.GetRoute)
		sacco.PUT("/routes/:id", controllers.UpdateRoute)              // For updating route metadata
        sacco.DELETE("/routes/:id", controllers.DeleteRoute)
	}

}