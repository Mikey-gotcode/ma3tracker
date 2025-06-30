// internal/models/driver.go
package models

import (
	//"time"

	"gorm.io/gorm"
)
type Driver struct {
    gorm.Model
    UserID          uint   `json:"user_id" gorm:"unique"` // Foreign key to User
    VehicleID     uint   `json:"vehicle_id" gorm:"index"`
    User            User   `gorm:"foreignKey:UserID"`     // User association
    Name            string `json:"name"`                  // Driver's specific name (if different from User.Name)
    Phone           string `json:"phone"`                 // Driver's specific phone (if different from User.Phone)
    LicenseNumber   string `json:"license_number"`
    SaccoID         uint   `json:"sacco_id"` // Foreign key to Sacco
    Sacco           Sacco  `gorm:"foreignKey:SaccoID"` // Sacco association
    // DO NOT include Email, Password, or Role here. They are in the User model.
}