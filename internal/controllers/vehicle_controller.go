package controllers

import (
	"errors" // Import for gorm.ErrRecordNotFound
	"net/http"
	"strconv" // Import for strconv.ParseUint

	"github.com/gin-gonic/gin"
	"gorm.io/gorm" // Import for GORM transaction and error handling

	"ma3_tracker/internal/config"
	"ma3_tracker/internal/models" // Your models package
)

// serviceStatusPayload defines the expected JSON for updating vehicle service status
// type serviceStatusPayload struct {
// 	InService bool `json:"in_service" binding:"required"`
// }

// CreateVehicle handles creating a new vehicle for a sacco, defaulting InService to true
func CreateVehicle(c *gin.Context) {
	// Input payload struct to receive data from the client
	var input struct {
		VehicleNo           string `json:"vehicle_no" binding:"required"`
		VehicleRegistration string `json:"vehicle_registration" binding:"required"`
		SaccoID       uint   `json:"sacco_id"`
		DriverID            uint   `json:"driver_id" binding:"required"`
		RouteID             uint   `json:"route_id" binding:"required"`
	}

	// Bind and validate JSON input from the request body
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input: " + err.Error()})
		return
	}

	// Extract the authenticated UserID from JWT claims. This is the ID of the user
	// who is making the request, which should be a Sacco owner in this context.
	authenticatedUserID := uint(c.MustGet("user_id").(float64))

	// Verify the authenticated user is indeed a Sacco owner and get their SaccoID.
	// We preload the Sacco association to get the actual Sacco ID from the saccos table.
	var saccoUser models.User
	if err := config.DB.Preload("Sacco").First(&saccoUser, authenticatedUserID).Error; err != nil {
		// If user not found or database error during fetch
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authenticated user not found or could not verify role."})
		return
	}
	// Check if the user's role is 'sacco' and if they have an associated Sacco profile.
	if saccoUser.Role != "sacco" || saccoUser.Sacco == nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only Sacco owners can create vehicles."})
		return
	}
	// Get the actual SaccoID from the associated Sacco model
	saccoID := saccoUser.Sacco.ID

	// Start a database transaction to ensure atomicity. If any step fails, everything is rolled back.
	tx := config.DB.Begin()
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not start transaction."})
		return
	}

	// 1. Validate DriverID: Ensure the driver exists AND belongs to this specific Sacco.
	var driver models.Driver
	if err := tx.Where("id = ? AND sacco_id = ?", input.DriverID, saccoID).First(&driver).Error; err != nil {
		tx.Rollback() // Rollback the transaction on validation failure
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Assigned Driver not found or does not belong to this Sacco."})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error validating driver: " + err.Error()})
		}
		return
	}

	// 2. Validate RouteID: Ensure the route exists AND belongs to this specific Sacco (assuming routes are Sacco-specific).
	var route models.Route
	// If routes can be shared across saccos, remove the `AND sacco_id = ?` part.
	if err := tx.Where("id = ? AND sacco_id = ?", input.RouteID, saccoID).First(&route).Error; err != nil {
		tx.Rollback() // Rollback the transaction on validation failure
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Assigned Route not found or does not belong to this Sacco."})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error validating route: " + err.Error()})
		}
		return
	}

	// Initialize the Vehicle model with the validated and correct IDs
	vehicle := models.Vehicle{
		VehicleNo:           input.VehicleNo,
		VehicleRegistration: input.VehicleRegistration,
		SaccoID:             saccoID,        // Use the validated SaccoID from the authenticated user's Sacco profile
		DriverID:            input.DriverID, // Use the validated DriverID from the request
		RouteID:             input.RouteID,  // Use the validated RouteID from the request
		InService:           true,           // Default to true
	}

	// Save the new vehicle record to the database within the transaction
	if err := tx.Create(&vehicle).Error; err != nil {
		tx.Rollback() // Rollback if creation fails
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create vehicle: " + err.Error()})
		return
	}

	// Commit the transaction if all operations were successful
	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not commit transaction: " + err.Error()})
		return
	}

	// Respond with the successfully created vehicle
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"message": "Vehicle created successfully.",
		"vehicle": vehicle,
	})
}

// GetMyVehicles retrieves vehicles based on the authenticated user's role (Sacco owner or Driver).
func GetMyVehicles(c *gin.Context) {
	userID := uint(c.MustGet("user_id").(float64))

	var user models.User
	// Preload Sacco and Driver to determine user's specific role context
	if err := config.DB.Preload("Sacco").Preload("Driver").First(&user, userID).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authenticated user not found."})
		return
	}

	var vehicles []models.Vehicle
	if user.Role == "sacco" && user.Sacco != nil {
		// If it's a Sacco owner, list vehicles belonging to their Sacco
		if err := config.DB.Where("sacco_id = ?", user.Sacco.ID).Find(&vehicles).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching vehicles for your Sacco: " + err.Error()})
			return
		}
	} else if user.Role == "driver" && user.Driver != nil {
		// If it's a driver, list vehicles assigned to this specific driver
		if err := config.DB.Where("driver_id = ?", user.Driver.ID).Find(&vehicles).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching vehicles assigned to you: " + err.Error()})
			return
		}
	} else {
		// For other roles (commuter) or inconsistent states, deny access. Admin should use ListVehicles.
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. You must be a Sacco owner or an assigned driver to view your vehicles."})
		return
	}

	c.JSON(http.StatusOK, gin.H{"vehicles": vehicles})
}

// ListVehicles is typically for administrative use, fetching all vehicles without specific filtering.
func ListVehicles(c *gin.Context) {
	var vehicles []models.Vehicle
	if err := config.DB.Find(&vehicles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error listing vehicles: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": vehicles})
}

// ListVehicles returns only vehicles that are currently in service (in_service = true).
func ListActiveVehicles(c *gin.Context) {
	var vehicles []models.Vehicle
	if err := config.DB.Where("in_service = ?", true).Find(&vehicles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error listing vehicles: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": vehicles})
}


func ListVehiclesBySacco(c *gin.Context) {
	// Get sacco_id from PATH parameter
	saccoIDStr := c.Param("id") // Extract 'id' from the URL path (e.g., /vehicles/123 -> id = "123")
	if saccoIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Sacco ID path parameter is required."})
		return
	}

	saccoID, err := strconv.ParseUint(saccoIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Sacco ID format in path parameter."})
		return
	}

	var vehicles []models.Vehicle // Slice to hold the fetched vehicles
	// Filter vehicles by the provided sacco_id
	if err := config.DB.Where("sacco_id = ?", uint(saccoID)).Find(&vehicles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error listing vehicles for sacco: " + err.Error()})
		return
	}

	// Respond with the list of vehicles, wrapped in a "data" key for consistency
	c.JSON(http.StatusOK, gin.H{"data": vehicles})
	
}

// UpdateVehicle allows modifying vehicle details, restricted to Sacco owners or Admins.
func UpdateVehicle(c *gin.Context) {
	authenticatedUserID := uint(c.MustGet("user_id").(float64))
	vehIDStr := c.Param("id")

	var user models.User
	if err := config.DB.Preload("Sacco").Preload("Driver").First(&user, authenticatedUserID).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authenticated user not found."})
		return
	}

	if user.Role != "sacco" && user.Role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only Sacco owners or administrators can update vehicles."})
		return
	}

	vehID, err := strconv.ParseUint(vehIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Vehicle ID format."})
		return
	}

	var vehicle models.Vehicle
	query := config.DB.Where("id = ?", uint(vehID))

	if user.Role == "sacco" && user.Sacco != nil {
		query = query.Where("sacco_id = ?", user.Sacco.ID)
	}

	if err := query.First(&vehicle).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Vehicle not found or not assigned to your Sacco."})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error fetching vehicle: " + err.Error()})
		}
		return
	}

	var updateInput struct {
		VehicleNo           *string `json:"vehicle_no"`
		VehicleRegistration *string `json:"vehicle_registration"`
		DriverID            *uint   `json:"driver_id"`
		RouteID             *uint   `json:"route_id"`
		InService           *bool   `json:"in_service"`
	}

	if err := c.ShouldBindJSON(&updateInput); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid update input: " + err.Error()})
		return
	}

	tx := config.DB.Begin()
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not start transaction."})
		return
	}

	if updateInput.VehicleNo != nil {
		vehicle.VehicleNo = *updateInput.VehicleNo
	}
	if updateInput.VehicleRegistration != nil {
		vehicle.VehicleRegistration = *updateInput.VehicleRegistration
	}
	if updateInput.InService != nil {
		vehicle.InService = *updateInput.InService
	}

	if updateInput.DriverID != nil {
		var newDriver models.Driver
		driverQuery := tx.Where("id = ?", *updateInput.DriverID)
		if user.Role == "sacco" && user.Sacco != nil {
			driverQuery = driverQuery.Where("sacco_id = ?", user.Sacco.ID)
		}
		if err := driverQuery.First(&newDriver).Error; err != nil {
			tx.Rollback()
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Assigned driver not found or does not belong to this Sacco."})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error validating new driver: " + err.Error()})
			}
			return
		}
		vehicle.DriverID = *updateInput.DriverID
	}

	if updateInput.RouteID != nil {
		var newRoute models.Route
		routeQuery := tx.Where("id = ?", *updateInput.RouteID)
		if user.Role == "sacco" && user.Sacco != nil {
			routeQuery = routeQuery.Where("sacco_id = ?", user.Sacco.ID)
		}
		if err := routeQuery.First(&newRoute).Error; err != nil {
			tx.Rollback()
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Assigned route not found or does not belong to this Sacco."})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error validating new route: " + err.Error()})
			}
			return
		}
		vehicle.RouteID = *updateInput.RouteID
	}

	if err := tx.Save(&vehicle).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update vehicle details: " + err.Error()})
		return
	}

	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not commit transaction: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Vehicle updated successfully", "vehicle": vehicle})
}

// DeleteVehicle removes a vehicle, restricted to Sacco owners or Admins.
func DeleteVehicle(c *gin.Context) {
	authenticatedUserID := uint(c.MustGet("user_id").(float64))
	vehIDStr := c.Param("id")

	var user models.User
	if err := config.DB.Preload("Sacco").First(&user, authenticatedUserID).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authenticated user not found."})
		return
	}

	if user.Role != "sacco" && user.Role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only Sacco owners or administrators can delete vehicles."})
		return
	}

	vehID, err := strconv.ParseUint(vehIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Vehicle ID format."})
		return
	}

	var vehicle models.Vehicle
	query := config.DB.Where("id = ?", uint(vehID))

	if user.Role == "sacco" && user.Sacco != nil {
		query = query.Where("sacco_id = ?", user.Sacco.ID)
	}

	if err := query.First(&vehicle).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Vehicle not found or not assigned to your Sacco."})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error fetching vehicle for deletion: " + err.Error()})
		}
		return
	}

	if err := config.DB.Delete(&vehicle).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete vehicle: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Vehicle deleted successfully."})
}