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
		
	}
}