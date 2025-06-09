package controllers

import (
	"ma3_tracker/internal/config"
	"ma3_tracker/internal/models"
	"github.com/gin-gonic/gin"
	"net/http"
)

// CreateVehicle allows a sacco to create a new vehicle; defaults InService to true
func CreateVehicle(c *gin.Context) {
	var input struct {
		VehicleNo           string `json:"vehicle_no" binding:"required"`
		VehicleRegistration string `json:"vehicle_registration" binding:"required"`
		// InService omitted: always default true on creation
	}

	// Parse JSON
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid vehicle input: " + err.Error()})
		return
	}

	// Get sacco ID from token claims
	saccoID := uint(c.MustGet("user_id").(float64))

	// Build vehicle with default InService=true
	vehicle := models.Vehicle{
		VehicleNo:           input.VehicleNo,
		VehicleRegistration: input.VehicleRegistration,
		SaccoID:             saccoID,
		InService:           false,
	}

	// Save to DB
	if err := config.DB.Create(&vehicle).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create vehicle: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"vehicle": vehicle})
}

func GetMyVehicles(c *gin.Context) {
	userID := c.MustGet("user_id").(float64)

	var vehicles []models.Vehicle
	if err := config.DB.Where("sacco_id = ?", uint(userID)).Find(&vehicles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching vehicles"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"vehicles": vehicles})
}

// This method is typically for administrative use.
func ListVehicles(c *gin.Context) {
	var vehicles []models.Vehicle
	// Fetch all vehicles without filtering by sacco_id
	if err := config.DB.Find(&vehicles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error listing vehicles: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": vehicles}) // Return in a 'data' key for consistency with frontend service
}

func UpdateVehicle(c *gin.Context) {
	userID := c.MustGet("user_id").(float64)
	id := c.Param("id")

	var vehicle models.Vehicle
	if err := config.DB.Where("id = ? AND sacco_id = ?", id, uint(userID)).First(&vehicle).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Vehicle not found"})
		return
	}

	if err := c.ShouldBindJSON(&vehicle); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid update"})
		return
	}

	config.DB.Save(&vehicle)
	c.JSON(http.StatusOK, gin.H{"vehicle": vehicle})
}

func DeleteVehicle(c *gin.Context) {
	userID := c.MustGet("user_id").(float64)
	id := c.Param("id")

	var vehicle models.Vehicle
	if err := config.DB.Where("id = ? AND sacco_id = ?", id, uint(userID)).First(&vehicle).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Vehicle not found"})
		return
	}

	config.DB.Delete(&vehicle)
	c.JSON(http.StatusOK, gin.H{"message": "Vehicle deleted"})
}
