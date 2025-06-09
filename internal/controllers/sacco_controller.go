package controllers

import (
	"ma3_tracker/internal/config"
	"ma3_tracker/internal/models"
	"net/http"

	"github.com/gin-gonic/gin"
)

// CreateSacco registers a new Sacco
func CreateSacco(c *gin.Context) {
	var input models.Sacco
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := config.DB.Create(&input).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create sacco: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"sacco": input})
}

// GetSacco retrieves a Sacco by ID
func GetSacco(c *gin.Context) {
	id := c.Param("id")
	var sacco models.Sacco
	if err := config.DB.Preload("Vehicles").First(&sacco, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Sacco not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"sacco": sacco})
}

// ListSaccos lists all Saccos
func ListSaccos(c *gin.Context) {
	var saccos []models.Sacco
	if err := config.DB.Find(&saccos).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not fetch saccos"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": saccos})
}

// UpdateSacco modifies an existing Sacco
func UpdateSacco(c *gin.Context) {
	id := c.Param("id")
	var sacco models.Sacco
	if err := config.DB.First(&sacco, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Sacco not found"})
		return
	}

	var input struct {
		Name    *string `json:"name"`
		Owner   *string `json:"owner"`
		Email   *string `json:"email"`
		Phone   *string `json:"phone"`
		Address *string `json:"address"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if input.Name != nil {
		sacco.Name = *input.Name
	}
	if input.Owner != nil {
		sacco.Owner = *input.Owner
	}
	if input.Email != nil {
		sacco.Email = *input.Email
	}
	if input.Phone != nil {
		sacco.Phone = *input.Phone
	}
	if input.Address != nil {
		sacco.Address = *input.Address
	}

	config.DB.Save(&sacco)
	c.JSON(http.StatusOK, gin.H{"sacco": sacco})
}

// DeleteSacco removes a Sacco by ID
func DeleteSacco(c *gin.Context) {
	id := c.Param("id")
	if err := config.DB.Delete(&models.Sacco{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete sacco"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Sacco deleted"})
}