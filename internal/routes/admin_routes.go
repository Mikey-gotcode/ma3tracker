package routes

import (
	"ma3_tracker/internal/controllers"
	"ma3_tracker/internal/middleware"

	"github.com/gin-gonic/gin"
)

func AdminRoutes(r *gin.Engine){
	admin := r.Group("admin")
	admin.Use(middleware.RequireAuthWithRole("admin"))
	{
		admin.GET("/saccos",controllers.ListSaccos)
		admin.GET("/vehicles",controllers.ListVehicles)
		admin.GET("/commuters",controllers.ListCommuters)
		admin.GET("/drivers",controllers.ListDrivers)

	}
}