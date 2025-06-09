package controllers

import (
	"ma3_tracker/internal/config"
	"ma3_tracker/internal/models"
	"net/http"
	"golang.org/x/crypto/bcrypt"
	"github.com/gin-gonic/gin"
)

// Payload for toggling service status
type serviceStatusPayload struct {
	InService bool `json:"in_service" binding:"required"`
}

// SetServiceStatus allows a driver to change their vehicle's in_service flag
func SetServiceStatus(c *gin.Context) {
	// 1) Get driver ID from JWT claims (float64 → uint)
	driverID := uint(c.MustGet("user_id").(float64))

	// 2) Parse vehicle ID from URL
	vehID := c.Param("id")

	// 3) Find the vehicle assigned to this driver
	var vehicle models.Vehicle
	if err := config.DB.
		Where("id = ? AND driver_id = ?", vehID, driverID).
		First(&vehicle).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Vehicle not found or not assigned to you"})
		return
	}

	// 4) Bind JSON payload
	var payload serviceStatusPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// 5) Update the in_service flag
	vehicle.InService = payload.InService
	if err := config.DB.Save(&vehicle).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update status"})
		return
	}

	// 6) Return updated vehicle
	c.JSON(http.StatusOK, gin.H{
		"message": "Service status updated",
		"vehicle": vehicle,
	})
}


// CreateDriver registers a new driver (role = "driver")
func CreateDriver(c *gin.Context) {
	var input models.Driver
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Set role explicitly
	input.Role = "driver"

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}
	input.Password = string(hash)

	if err := config.DB.Create(&input).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create driver: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"driver": input})
}

// GetDriver fetches a single driver by ID
func GetDriver(c *gin.Context) {
	id := c.Param("id")
	var driver models.Driver
	if err := config.DB.First(&driver, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Driver not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"driver": driver})
}

// This method is typically for administrative use.
func ListDrivers(c *gin.Context) {
	var drivers []models.User // Assuming Driver model is an embedded User model or has similar fields
	// Fetch all users where the role is 'driver'
	if err := config.DB.Where("role = ?", "driver").Find(&drivers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error listing drivers: " + err.Error()})
		return
	}

	// For security, you might want to omit the password hash from the response
	// by either creating a DTO (Data Transfer Object) or manually selecting fields.
	// For simplicity, we are returning the full User model, but be mindful of sensitive data.
	// Optionally, clear passwords for the response:
	for i := range drivers {
		drivers[i].Password = ""
	}

	c.JSON(http.StatusOK, gin.H{"data": drivers}) // Return in a 'data' key for consistency with frontend service
}

// UpdateDriver allows modifying driver details
func UpdateDriver(c *gin.Context) {
	id := c.Param("id")
	var driver models.Driver
	if err := config.DB.First(&driver, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Driver not found"})
		return
	}

	var input struct {
		Name     *string `json:"name"`
		Email    *string `json:"email"`
		Phone    *string `json:"phone"`
		Password *string `json:"password"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if input.Name != nil {
		driver.Name = *input.Name
	}
	if input.Email != nil {
		driver.Email = *input.Email
	}
	if input.Phone != nil {
		driver.Phone = *input.Phone
	}
	if input.Password != nil {
		hash, _ := bcrypt.GenerateFromPassword([]byte(*input.Password), bcrypt.DefaultCost)
		driver.Password = string(hash)
	}

	config.DB.Save(&driver)
	c.JSON(http.StatusOK, gin.H{"driver": driver})
}

// DeleteDriver removes a driver by ID
func DeleteDriver(c *gin.Context) {
	id := c.Param("id")
	if err := config.DB.Delete(&models.Driver{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete driver"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Driver deleted"})
}
