package models

import (
	"gorm.io/gorm"
)

// Route represents a service path operated by a sacco
// A sacco can have multiple routes; each route has many stages and assigned vehicles
type Route struct {
	gorm.Model

	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	SaccoID     uint     `json:"sacco_id"`

	// Geometry stored in PostGIS as a LINESTRING (SRID 4326)
	// When creating, provide GeoJSON; migrations define the column type appropriately.
	Geometry    []byte  `gorm:"type:bytea"`

	// Associations
	Stages      []Stage  `gorm:"foreignKey:RouteID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"stages,omitempty"`
	Vehicles    []Vehicle`gorm:"foreignKey:RouteID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"vehicles,omitempty"`
}
