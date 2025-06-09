// internal/models/driver.go
package models

import (
	"time"

	"gorm.io/gorm"
)

// Driver represents a system user with role="driver"
type Driver struct {
	gorm.Model
	ID        uint           `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	UserID uint `gorm:"uniqueIndex" json:"user_id"`

	Name      string         `json:"name" binding:"required"`
	Email     string         `gorm:"unique;not null" json:"email" binding:"required,email"`
	Password  string         `json:"password" binding:"required"`
	Phone     string         `json:"phone"`
	Role      string         `json:"role" gorm:"default:driver"`

	// Optional: Link to Vehicle if one-to-one
	Vehicle   Vehicle        `gorm:"foreignKey:DriverID" json:"vehicle,omitempty"`
	VehicleID uint           `json:"vehicle_id"`
}