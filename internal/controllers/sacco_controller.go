package controllers

import (
	"errors" // Import errors for gorm.ErrRecordNotFound
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"ma3_tracker/internal/config"
	"ma3_tracker/internal/models" // Your models package
)

// updateSaccoInput defines the fields a client can send to update a Sacco's profile.
type updateSaccoInput struct {
    Name    *string `json:"name"`
    Owner   *string `json:"owner"` // Corrected: Should be 'owner' not 'name' if separate from Name
    Email   *string `json:"email"`
    Phone   *string `json:"phone"`
    Address *string `json:"address"` // Assuming Sacco model has an Address field
}

// --- Sacco Controller Functions ---

// CreateSacco (Commented out):
// As per the discussion, new Sacco creation as a new user with 'sacco' role
// is handled by `SignupUser` in `auth_controller.go`. If this endpoint
// is for an admin to create a Sacco *without* creating a new User for it
// (e.g., adding an external Sacco), its logic would need to be re-evaluated
// to *not* create an associated User record, or to link to an existing User.
// For now, it's removed to avoid conflicting with the `SignupUser` flow.
/*
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
*/

// GetSacco retrieves a Sacco by ID and preloads its associated Vehicles.
func GetSacco(c *gin.Context) {
    saccoIDStr := c.Param("id")
    saccoID, err := strconv.ParseUint(saccoIDStr, 10, 32)
    if err != nil {
        logrus.WithError(err).Warnf("GetSacco: invalid sacco ID '%s'", saccoIDStr)
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Sacco ID format."})
        return
    }

    var sacco models.Sacco
    if err := config.DB.Preload("Vehicles").Preload("User").First(&sacco, uint(saccoID)).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            logrus.WithField("sacco_id", saccoID).Info("GetSacco: sacco not found")
            c.JSON(http.StatusNotFound, gin.H{"error": "Sacco not found."})
        } else {
            logrus.WithError(err).WithField("sacco_id", saccoID).Error("GetSacco: database error fetching Sacco")
            c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error fetching Sacco."})
        }
        return
    }

    response := gin.H{
        "ID":        sacco.ID,
        "CreatedAt": sacco.CreatedAt,
        "UpdatedAt": sacco.UpdatedAt,
        "name":      sacco.Name,
        "owner":     sacco.Owner,
        "email":     sacco.Email,
        "phone":     sacco.Phone,
        "vehicles":  sacco.Vehicles,
    }
    if sacco.User != nil && sacco.User.ID != 0 {
        response["owner_user_details"] = gin.H{
            "id":    sacco.User.ID,
            "name":  sacco.User.Name,
            "email": sacco.User.Email,
            "phone": sacco.User.Phone,
        }
    }

    logrus.WithField("sacco_id", saccoID).Info("GetSacco: retrieved sacco successfully")
    c.JSON(http.StatusOK, gin.H{"sacco": response})
}

// ListDriversBySacco fetches drivers for a given sacco_id query param.
func ListDriversBySacco(c *gin.Context) {
    saccoIDStr := c.Param("id")
    if saccoIDStr == "" {
        logrus.Warn("ListDriversBySacco: missing sacco_id query param")
        c.JSON(http.StatusBadRequest, gin.H{"error": "sacco_id query parameter is required."})
        return
    }
    saccoID, err := strconv.ParseUint(saccoIDStr, 10, 32)
    if err != nil {
        logrus.WithError(err).Warnf("ListDriversBySacco: invalid sacco_id '%s'", saccoIDStr)
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Sacco ID format."})
        return
    }

    var drivers []models.Driver
    if err := config.DB.Where("sacco_id = ?", uint(saccoID)).Preload("User").Find(&drivers).Error; err != nil {
        logrus.WithError(err).WithField("sacco_id", saccoID).Error("ListDriversBySacco: error listing drivers")
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Error listing drivers for sacco."})
        return
    }

    var profiles []gin.H
    for _, d := range drivers {
        profile := gin.H{
            "ID":             d.ID,
            "name":           d.Name,
            "phone":          d.Phone,
            "license_number": d.LicenseNumber,
            "sacco_id":       d.SaccoID,
        }
        if d.User.ID != 0 {
            profile["user_details"] = gin.H{
                "id":    d.User.ID,
                "name":  d.User.Name,
                "email": d.User.Email,
                "phone": d.User.Phone,
                "role":  d.User.Role,
            }
        }
        profiles = append(profiles, profile)
    }

    logrus.WithField("sacco_id", saccoID).Infof("ListDriversBySacco: found %d drivers", len(profiles))
    c.JSON(http.StatusOK, gin.H{"data": profiles})
}

// ListSaccos returns all saccos with associated user and vehicles.
func ListSaccos(c *gin.Context) {
    var saccos []models.Sacco
    if err := config.DB.Preload("User").Preload("Vehicles").Find(&saccos).Error; err != nil {
        logrus.WithError(err).Error("ListSaccos: could not fetch saccos")
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not fetch saccos."})
        return
    }

    var out []gin.H
    for _, s := range saccos {
        item := gin.H{
            "ID":        s.ID,
            "CreatedAt": s.CreatedAt,
            "UpdatedAt": s.UpdatedAt,
            "name":      s.Name,
            "owner":     s.Owner,
            "email":     s.Email,
            "phone":     s.Phone,
            "vehicles":  s.Vehicles,
        }
        if s.User != nil && s.User.ID != 0 {
            item["owner_user_details"] = gin.H{
                "id":    s.User.ID,
                "name":  s.User.Name,
                "email": s.User.Email,
                "phone": s.User.Phone,
            }
        }
        out = append(out, item)
    }

    logrus.Infof("ListSaccos: returned %d saccos", len(out))
    c.JSON(http.StatusOK, gin.H{"data": out})
}

// UpdateSacco modifies an existing Sacco's details.
func UpdateSacco(c *gin.Context) {
    saccoIDStr := c.Param("id")
    saccoID, err := strconv.ParseUint(saccoIDStr, 10, 32)
    if err != nil {
        logrus.WithError(err).Warnf("UpdateSacco: invalid sacco_id '%s'", saccoIDStr)
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Sacco ID format."})
        return
    }

    var sacco models.Sacco
    if err := config.DB.First(&sacco, uint(saccoID)).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            logrus.WithField("sacco_id", saccoID).Info("UpdateSacco: sacco not found")
            c.JSON(http.StatusNotFound, gin.H{"error": "Sacco not found."})
        } else {
            logrus.WithError(err).WithField("sacco_id", saccoID).Error("UpdateSacco: database error")
            c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error updating Sacco."})
        }
        return
    }

    var input updateSaccoInput
    if err := c.ShouldBindJSON(&input); err != nil {
        logrus.WithError(err).Warn("UpdateSacco: invalid request body")
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body."})
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

    if err := config.DB.Save(&sacco).Error; err != nil {
        logrus.WithError(err).WithField("sacco_id", saccoID).Error("UpdateSacco: save failed")
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update sacco."})
        return
    }

    logrus.WithField("sacco_id", saccoID).Info("UpdateSacco: sacco updated successfully")
    c.JSON(http.StatusOK, gin.H{"message": "Sacco updated successfully", "sacco": sacco})
}

// DeleteSacco removes a Sacco by ID.
func DeleteSacco(c *gin.Context) {
    saccoIDStr := c.Param("id")
    saccoID, err := strconv.ParseUint(saccoIDStr, 10, 32)
    if err != nil {
        logrus.WithError(err).Warnf("DeleteSacco: invalid sacco_id '%s'", saccoIDStr)
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Sacco ID format."})
        return
    }

    var sacco models.Sacco
    if err := config.DB.First(&sacco, uint(saccoID)).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            logrus.WithField("sacco_id", saccoID).Info("DeleteSacco: sacco not found")
            c.JSON(http.StatusNotFound, gin.H{"error": "Sacco not found."})
        } else {
            logrus.WithError(err).WithField("sacco_id", saccoID).Error("DeleteSacco: database error fetching Sacco for deletion")
            c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error fetching Sacco for deletion."})
        }
        return
    }

    if err := config.DB.Delete(&sacco).Error; err != nil {
        logrus.WithError(err).WithField("sacco_id", saccoID).Error("DeleteSacco: delete failed")
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete sacco."})
        return
    }

    logrus.WithField("sacco_id", saccoID).Info("DeleteSacco: sacco deleted successfully")
    c.JSON(http.StatusOK, gin.H{"message": "Sacco deleted successfully."})
}