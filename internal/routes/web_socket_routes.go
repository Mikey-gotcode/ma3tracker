package routes

import (
	//"ma3_tracker/internal/controllers"
	//"ma3_tracker/internal/middleware"
	"ma3_tracker/internal/controllers"
	"github.com/gin-gonic/gin"
)


func WebSocketRoutes (r *gin.Engine){
	wsRoutes := r.Group("/ws")
	wsRoutes.Use()
	{

		wsRoutes.GET("/location", controllers.HandleLocationWebSocket) // <--- NEW WEBSOCKET ROUTE

	}
}