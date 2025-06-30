package models

import (
	"time"
	"gorm.io/gorm"
)

type LocationHistory struct {
	gorm.Model
	DriverID    uint      `json:"driver_id" gorm:"index"`
	Driver      Driver    `gorm:"foreignKey:DriverID"`
	Latitude    float64   `json:"latitude"`
	Longitude   float64   `json:"longitude"`
	Accuracy    float64   `json:"accuracy"`    // GPS accuracy in meters
	Speed       float64   `json:"speed"`       // Speed in km/h
	Bearing     float64   `json:"bearing"`     // Direction in degrees
	Altitude    float64   `json:"altitude"`    // Altitude in meters
	IsMoving    bool      `json:"is_moving"`   // Movement status
	DistanceFromLast float64 `json:"distance_from_last"` // Distance from previous point
	Timestamp   time.Time `json:"timestamp"`
	EventType   string    `json:"event_type"` // "start", "moving", "stopped", "idle", "significant_movement"
}
