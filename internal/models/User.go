package models

import "gorm.io/gorm"

type User struct {
	gorm.Model
	Name     string `json:"name"`
	Email    string `json:"email" gorm:"unique"`
	Password string `json:"password"`
	Phone    string `json:"phone"`
	Role     string `json:"role"` // "commuter", "driver", "sacco", "admin"

	// Actor-specific relations
	Sacco     *Sacco         `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"sacco,omitempty"`
	Driver    *Driver        `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"driver,omitempty"`
}
