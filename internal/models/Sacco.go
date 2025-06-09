// internal/models/sacco.go
package models

import (
	"time"

	"gorm.io/gorm"
)

// Sacco represents a transport company or cooperative entity
// that operates vehicles on various routes.
type Sacco struct {
	gorm.Model
	ID        uint           `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	UserID uint `gorm:"uniqueIndex" json:"user_id"`

	Name      string         `json:"name" binding:"required"`
	Owner     string         `json:"owner" binding:"required"`
	Email     string         `gorm:"unique;not null" json:"email" binding:"required,email"`
	Phone     string         `json:"phone"`
	Address   string         `json:"address"`

	Vehicles  []Vehicle      `gorm:"foreignKey:SaccoID" json:"vehicles,omitempty"`
}
