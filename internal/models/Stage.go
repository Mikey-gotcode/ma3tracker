package models

import (
	"gorm.io/gorm"
)

// Stage represents a stop or dropoff location along a route
// Sequence indicates order and optional geographic coordinates
type Stage struct {
	gorm.Model

	Name    string  `json:"name" binding:"required"`
	Seq     int     `json:"seq" binding:"required"`
	Lat     float64 `json:"lat" binding:"required"`
	Lng     float64 `json:"lng" binding:"required"`

	// Foreign key to route
	RouteID uint    `json:"route_id"`
}
