package routes

import (
	"ma3_tracker/internal/controllers"
	"ma3_tracker/internal/middleware"
	"github.com/gin-gonic/gin"
)

func DriverRoutes (r *gin.Engine){
	driver := r.Group("/driver")
	driver.Use(middleware.RequireAuthWithRole("driver"))
	{
		driver.POST("/",controllers.CreateDriver)
		//driver.POST("/",controllers.CreateDriver)
		//driver.POST("/",controllers.CreateDriver)
		//driver.POST("/",controllers.CreateDriver)
	}
}