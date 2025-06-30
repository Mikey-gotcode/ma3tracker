package controllers

import (
	"errors" // Import errors for gorm.ErrRecordNotFound
	"net/http"
	"strconv" // For parsing IDs
	"gorm.io/gorm"

	 logrus "github.com/sirupsen/logrus"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt" // Used for password hashing

	"ma3_tracker/internal/config"
	"ma3_tracker/internal/models" // Your models package
)

// --- Helper Structs for Request Bodies ---

// serviceStatusPayload defines the expected JSON for updating vehicle service status.
type serviceStatusPayload struct {
	InService bool `json:"in_service" binding:"required"`
}

// updateDriverInput defines the fields a client can send to update a driver's profile.
// Note: Fields that belong to the User model will be updated on the associated User.
type updateDriverInput struct {
	UserName      *string `json:"name"`           // Optional: User's name
	UserEmail     *string `json:"email"`          // Optional: User's email
	UserPhone     *string `json:"phone"`          // Optional: User's general phone
	UserPassword  *string `json:"password"`       // Optional: User's password

	DriverPhone   *string `json:"driver_phone"`   // Optional: Driver-specific phone
	LicenseNumber *string `json:"license_number"` // Optional: Driver's license number
	SaccoID       *uint   `json:"sacco_id"`       // Optional: Driver's assigned Sacco ID
}

// --- Driver Controller Functions ---

// SetServiceStatus allows a driver to change their vehicle's in_service flag.
// Requires driver's user_id from JWT claims and vehicle ID from URL parameter.
func SetServiceStatus(c *gin.Context) {
	// 1) Get driver ID from JWT claims. This is actually the UserID of the authenticated user.
	userID := uint(c.MustGet("user_id").(float64))

	// 2) Parse vehicle ID from URL parameter.
	vehIDStr := c.Param("id")
	vehID, err := strconv.ParseUint(vehIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Vehicle ID format."})
		return
	}

	// 3) Find the Vehicle. Crucially, find the Vehicle AND ensure it's linked to the Driver
	//    who is linked to the authenticated UserID.
	var vehicle models.Vehicle
	// Join Vehicle with Driver, then Driver with User to verify ownership
	if err := config.DB.
		Joins("Driver"). // Assuming Vehicle has a DriverID and a Driver association
		Where("vehicles.id = ?", vehID).
		Where("Driver.user_id = ?", userID).
		First(&vehicle).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Vehicle not found or not assigned to you."})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error while fetching vehicle: " + err.Error()})
		}
		return
	}

	// 4) Bind JSON payload for the service status.
	var payload serviceStatusPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body format: " + err.Error()})
		return
	}

	// 5) Update the in_service flag and save the vehicle.
	vehicle.InService = payload.InService
	if err := config.DB.Save(&vehicle).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update service status: " + err.Error()})
		return
	}

	// 6) Return updated vehicle.
	c.JSON(http.StatusOK, gin.H{
		"message": "Service status updated successfully.",
		"vehicle": vehicle,
	})
}
// This endpoint might be used by a driver to fetch their own vehicle.
func GetVehicleByDriverID(c *gin.Context) {
    driverIDStr := c.Param("driverId")
    driverID, err := strconv.ParseUint(driverIDStr, 10, 64)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid driver ID format"})
        return
    }

    var vehicle models.Vehicle
    // Preload Driver to ensure the relation is established if needed in response
    if err := config.DB.Preload("Driver").Where("driver_id = ?", uint(driverID)).First(&vehicle).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            c.JSON(http.StatusNotFound, gin.H{"error": "Vehicle not found for this driver ID."})
            return
        }
        logrus.WithError(err).Error("Error fetching vehicle by driver ID from database")
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch vehicle data."})
        return
    }

    c.JSON(http.StatusOK, gin.H{"vehicle": vehicle})
}

// Assuming you have access to config.DB and a way to get the driverID from JWT
// (e.g., from c.MustGet("user_id").(float64))

// GetAuthenticatedDriverVehicle fetches the vehicle assigned to the authenticated driver.
func GetAuthenticatedDriverVehicle(c *gin.Context) {
    driverID := uint(c.MustGet("user_id").(float64)) // Assuming user_id in JWT is the driver's ID
    var vehicle models.Vehicle
    if err := config.DB.Where("driver_id = ?", driverID).First(&vehicle).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            c.JSON(http.StatusNotFound, gin.H{"error": "No vehicle assigned to this driver."})
            return
        }
        logrus.WithError(err).Error("Error fetching vehicle for authenticated driver")
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch vehicle data."})
        return
    }
    c.JSON(http.StatusOK, gin.H{"vehicle": vehicle})
}

// GetDriver fetches a single driver by their UserID.
// This endpoint typically takes the UserID associated with the driver.
func GetDriver(c *gin.Context) {
	// The ID in the URL parameter is the UserID of the driver.
	userIDStr := c.Param("id")
	userID, err := strconv.ParseUint(userIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid User ID format."})
		return
	}

	var user models.User
	// Preload the Driver and its Sacco association when fetching the User.
	if err := config.DB.Where("id = ? AND role = ?", uint(userID), "driver").
		Preload("Driver").
		Preload("Driver.Sacco").
		First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Driver user not found."})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error: " + err.Error()})
		}
		return
	}

	// Prepare the response using the helper function from auth_controller logic.
	response := prepareUserResponse(user) // This function should be accessible or copied here.
	c.JSON(http.StatusOK, gin.H{"driver_profile": response})
}


// ListDrivers fetches all users with the role 'driver' and preloads their driver profiles.
func ListDrivers(c *gin.Context) {
	var users []models.User // Fetching User records with role 'driver'
	// Preload Driver and its Sacco association for each user.
	if err := config.DB.Where("role = ?", "driver").
		Preload("Driver").
		Preload("Driver.Sacco").
		Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error listing drivers: " + err.Error()})
		return
	}

	// Prepare a list of driver profiles for the response.
	var driverProfiles []gin.H
	for _, user := range users {
		// Use the helper to prepare the response for each user.
		driverProfiles = append(driverProfiles, prepareUserResponse(user))
	}

	c.JSON(http.StatusOK, gin.H{"data": driverProfiles})
}

// UpdateDriver allows modifying driver details (both user-level and driver-specific).
func UpdateDriver(c *gin.Context) {
	// Get the UserID of the driver to be updated from the URL parameter.
	userIDStr := c.Param("id")
	userID, err := strconv.ParseUint(userIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid User ID format."})
		return
	}

	var user models.User
	// Fetch the User and preload the Driver association.
	if err := config.DB.Where("id = ? AND role = ?", uint(userID), "driver").
		Preload("Driver").
		First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Driver user not found."})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error fetching user: " + err.Error()})
		}
		return
	}

	var input updateDriverInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// Start a transaction for atomicity
	tx := config.DB.Begin()
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not start transaction."})
		return
	}

	// Update User-level fields if provided
	if input.UserName != nil {
		user.Name = *input.UserName
	}
	if input.UserEmail != nil {
		user.Email = *input.UserEmail
	}
	if input.UserPhone != nil {
		user.Phone = *input.UserPhone
	}
	if input.UserPassword != nil {
		hashedPassword, hashErr := bcrypt.GenerateFromPassword([]byte(*input.UserPassword), bcrypt.DefaultCost)
		if hashErr != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password."})
			return
		}
		user.Password = string(hashedPassword)
	}

	if err := tx.Save(&user).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user details: " + err.Error()})
		return
	}

	// Update Driver-specific fields if provided (only if a Driver profile exists for this user)
	if user.Driver != nil {
		if input.DriverPhone != nil {
			user.Driver.Phone = *input.DriverPhone
		}
		if input.LicenseNumber != nil {
			user.Driver.LicenseNumber = *input.LicenseNumber
		}
		if input.SaccoID != nil {
			// Validate if the new SaccoID exists
			var newSacco models.Sacco
			if err := tx.First(&newSacco, *input.SaccoID).Error; err != nil {
				tx.Rollback()
				if errors.Is(err, gorm.ErrRecordNotFound) {
					c.JSON(http.StatusBadRequest, gin.H{"error": "New Sacco ID provided does not exist."})
				} else {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error validating Sacco ID: " + err.Error()})
				}
				return
			}
			user.Driver.SaccoID = *input.SaccoID
		}

		if err := tx.Save(user.Driver).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update driver specific details: " + err.Error()})
			return
		}
	} else {
		// If role is driver but Driver profile is missing, this is an inconsistency.
		// You might want to handle this as an error or log it. For now, just continue.
	}

	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not commit transaction: " + err.Error()})
		return
	}

	// Re-fetch the user with all associations to send an accurate response
	var updatedUser models.User
	config.DB.Where("id = ?", user.ID).
		Preload("Driver").
		Preload("Driver.Sacco").
		First(&updatedUser) // Error ignored here as it was already checked above

	response := prepareUserResponse(updatedUser)
	c.JSON(http.StatusOK, gin.H{
		"message":      "Driver details updated successfully.",
		"driver_profile": response,
	})
}

// UpdateVehicleStatus updates the in_service status of a vehicle.
// UpdateVehicleStatus updates the in_service status of a vehicle.
func UpdateVehicleStatus(c *gin.Context) {
    vehicleID, err := strconv.ParseUint(c.Param("id"), 10, 64)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid vehicle ID"})
        return
    }

    var input struct {
        InService *bool `json:"in_service"` // Use pointer to differentiate between missing and false
    }
    if err := c.ShouldBindJSON(&input); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input: " + err.Error()})
        return
    }

    var vehicle models.Vehicle
    if err := config.DB.First(&vehicle, vehicleID).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            c.JSON(http.StatusNotFound, gin.H{"error": "Vehicle not found"})
            return
        }
        logrus.WithError(err).Error("Database error fetching vehicle for update")
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch vehicle"})
        return
    }

    // 1. Get the authenticated User.ID from the JWT
    // This is User.ID (e.g., 33)
    authenticatedUserID := uint(c.MustGet("user_id").(float64))

    // 2. Find the Driver profile associated with this User.ID
    var driverProfile models.Driver
    // Assuming your Driver model has a UserID field linking it to the User model
    if err := config.DB.Where("user_id = ?", authenticatedUserID).First(&driverProfile).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            // This user is authenticated, but no driver profile is linked to them.
            // Or the driver profile lookup failed.
            c.JSON(http.StatusUnauthorized, gin.H{"error": "Driver profile not found for the authenticated user."})
            return
        }
        logrus.WithError(err).Error("Database error fetching driver profile for authorization")
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify authorization."})
        return
    }

    // 3. Now, compare the vehicle's DriverID with the ID of the found driver profile
    // This compares Driver.ID (e.g., 10) with Vehicle.DriverID (which should be 10)
    if vehicle.DriverID != driverProfile.ID {
        c.JSON(http.StatusForbidden, gin.H{"error": "You are not authorized to update this vehicle. It is assigned to a different driver."})
        return
    }

    // If authorization passes, proceed with update
    if input.InService != nil {
        vehicle.InService = *input.InService
    }

    if err := config.DB.Save(&vehicle).Error; err != nil {
        logrus.WithError(err).Error("Failed to save vehicle status update")
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update vehicle status"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"message": "Vehicle status updated successfully", "vehicle": vehicle})
}

// DeleteDriver removes a driver by their UserID. This will delete the User and
// cascade delete the associated Driver record due to GORM constraints.
func DeleteDriver(c *gin.Context) {
	// Get the UserID of the driver to be deleted from the URL parameter.
	userIDStr := c.Param("id")
	userID, err := strconv.ParseUint(userIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid User ID format."})
		return
	}

	// Fetch the User to ensure they exist and have the 'driver' role.
	var user models.User
	if err := config.DB.Where("id = ? AND role = ?", uint(userID), "driver").First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Driver user not found."})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error fetching user for deletion: " + err.Error()})
		}
		return
	}

	// Delete the User record. Due to GORM's `OnDelete:SET NULL` or `OnDelete:CASCADE` on the
	// Driver's UserID foreign key, the associated Driver record will be handled appropriately.
	// If it's CASCADE, the Driver record will also be deleted. If SET NULL, Driver.UserID will be nullified.
	// For a complete "driver removal", CASCADE delete is usually desired.
	// Ensure your model definitions have `OnDelete:CASCADE` for Driver's UserID.
	if err := config.DB.Delete(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete driver user: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Driver and associated user account deleted successfully."})
}

// --- Helper Functions (copied from auth_controller for completeness, but ideally in a separate utils/helpers file) ---

// prepareUserResponse constructs the JSON response map for the user, including nested actor details.
// This function needs access to the models.Sacco and models.Driver structs with their updated fields.
