package models

import (
	"gorm.io/gorm"
)

// Sacco represents a transport company or cooperative entity
// that operates vehicles on various routes.
type Sacco struct {
    gorm.Model
    UserID    uint      `json:"user_id" gorm:"unique"` // Foreign key to the User who owns this Sacco
    User      *User     `json:"user,omitempty" gorm:"foreignKey:UserID"` // Association with User (the Sacco owner's User profile)
    Name      string    `json:"name" gorm:"unique"`
    Owner     string    `json:"owner_name"` // Assuming this is the name of the owner, distinct from User.Name if UserID is separate.
    Email     string    `json:"email"`
    Phone     string    `json:"phone"`
    Address   string    `json:"address,omitempty"` // Add this field if you intend to use `sacco.Address`
    Vehicles  []Vehicle `json:"vehicles,omitempty" gorm:"foreignKey:SaccoID"` // One-to-Many association with Vehicles
}