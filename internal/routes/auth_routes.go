package routes

import (
	"ma3_tracker/internal/controllers"
	"ma3_tracker/internal/middleware"
	"github.com/gin-gonic/gin"
)

func AuthRoutes(r *gin.Engine) {
	auth := r.Group("/auth")
	{
		auth.POST("/signup", controllers.SignupUser)
		auth.POST("/login", controllers.LoginUser)
	}

	protected := r.Group("/api")
    protected.Use(middleware.RequireAuth()) // Use your RequireAuth middleware here
    {
        protected.PATCH("/profile", controllers.UpdateUserDetails)
        protected.GET("/profile", controllers.GetMyProfile) // <-- ADD THIS LINE
        protected.PUT("/change-password", controllers.ChangePassword)
    }
}
