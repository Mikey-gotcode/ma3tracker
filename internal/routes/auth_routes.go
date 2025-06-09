package routes

import (
	"ma3_tracker/internal/controllers"
	"github.com/gin-gonic/gin"
)

func AuthRoutes(r *gin.Engine) {
	auth := r.Group("/auth")
	{
		auth.POST("/signup", controllers.SignupUser)
		auth.POST("/login", controllers.LoginUser)
	}
}
