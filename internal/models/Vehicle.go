// internal/models/vehicle.go
package models

import (
	"gorm.io/gorm"
)

type Vehicle struct {
	gorm.Model
	VehicleNo               string `json:"vehicle_no"`
	VehicleRegistration     string `json:"vehicle_registration"`
	SaccoID                 uint   `json:"sacco_id"`
	DriverID                uint   `json:"driver_id"`               // link to the driver user
	InService               bool   `json:"in_service" gorm:"default:true"`
	 // ← add this so Route.Vehicles works
    RouteID             uint   `json:"route_id"`
}
