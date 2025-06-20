package controllers

import (
	"encoding/binary"
	"errors"
	"net/http"
	"strconv"
	"time" // Added for time.Time in RouteResponse

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"ma3_tracker/internal/config"
	"ma3_tracker/internal/models"

	"github.com/twpayne/go-geom"
	gjson "github.com/twpayne/go-geom/encoding/geojson"
	"github.com/twpayne/go-geom/encoding/wkb"
)

// RouteResponse struct for API output
// This mirrors models.Route but has Geometry as a string for JSON output
type RouteResponse struct {
	ID          uint            `json:"ID"`
	CreatedAt   time.Time       `json:"CreatedAt"`
	UpdatedAt   time.Time       `json:"UpdatedAt"`
	DeletedAt   gorm.DeletedAt  `json:"DeletedAt,omitempty"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	SaccoID     uint            `json:"sacco_id"`
	Geometry    string          `json:"geometry"` // This will hold the GeoJSON string for API response
	Stages      []models.Stage  `json:"stages"`
	Vehicles    []models.Vehicle `json:"vehicles"`
}

// toRouteResponse converts a models.Route to a RouteResponse
func toRouteResponse(route models.Route) RouteResponse {
    jsonGeom, _ := convertWKBToGeoJSON(route.Geometry) // Convert WKB to GeoJSON string
    return RouteResponse{
        ID:          route.ID,
        CreatedAt:   route.CreatedAt,
        UpdatedAt:   route.UpdatedAt,
        DeletedAt:   route.DeletedAt,
        Name:        route.Name,
        Description: route.Description,
        SaccoID:     route.SaccoID,
        Geometry:    jsonGeom, // Assign the GeoJSON string here
        Stages:      route.Stages,
        Vehicles:    route.Vehicles,
    }
}

// parseAndConvertGeometry parses a GeoJSON string into a geom.T and returns WKB bytes
func parseAndConvertGeometry(raw string) ([]byte, error) {
	if raw == "" {
		return nil, nil
	}
	var g geom.T
	err := gjson.Unmarshal([]byte(raw), &g)
	if err != nil {
		return nil, err
	}
	return wkb.Marshal(g, binary.LittleEndian) // Corrected: Pass binary.LittleEndian
}

// convertWKBToGeoJSON converts WKB bytes into a GeoJSON string
func convertWKBToGeoJSON(wkbBytes []byte) (string, error) {
	if len(wkbBytes) == 0 {
		return "", nil
	}
	g, err := wkb.Unmarshal(wkbBytes)
	if err != nil {
		return "", err
	}
	b, err := gjson.Marshal(g)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CreateRoute allows a sacco to create a new route with GeoJSON LineString and stages.
func CreateRoute(c *gin.Context) {
    var input struct {
        Name        string `json:"name" binding:"required"`
        Description string `json:"description"`
        Geometry    string `json:"geometry"` // Input is still a GeoJSON string
        Stages      []struct {
            Name string  `json:"name"`
            Seq  int     `json:"seq"`
            Lat  float64 `json:"lat"`
            Lng  float64 `json:"lng"`
        } `json:"stages"`
    }

    if err := c.ShouldBindJSON(&input); err != nil {
        logrus.WithError(err).Warn("CreateRoute: invalid input payload")
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input: " + err.Error()})
        return
    }

    authenticatedUserID := uint(c.MustGet("user_id").(float64))
    var saccoUser models.User
    if err := config.DB.Preload("Sacco").First(&saccoUser, authenticatedUserID).Error; err != nil {
        logrus.WithError(err).Error("CreateRoute: user not found or unauthorized")
        c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authorized"})
        return
    }
    if saccoUser.Role != "sacco" || saccoUser.Sacco == nil {
        c.JSON(http.StatusForbidden, gin.H{"error": "Only sacco owners can create routes"})
        return
    }
    saccoID := saccoUser.Sacco.ID

    tx := config.DB.Begin()
    if tx.Error != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
        return
    }

    wkbGeom, err := parseAndConvertGeometry(input.Geometry)
    if err != nil {
        tx.Rollback()
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid geometry: " + err.Error()})
        return
    }

    route := models.Route{Name: input.Name, Description: input.Description, SaccoID: saccoID, Geometry: wkbGeom} // Save as []byte
    if err := tx.Create(&route).Error; err != nil {
        tx.Rollback()
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Create route failed: " + err.Error()})
        return
    }

    for _, s := range input.Stages {
        stage := models.Stage{Name: s.Name, Seq: s.Seq, Lat: s.Lat, Lng: s.Lng, RouteID: route.ID}
        if err := tx.Create(&stage).Error; err != nil {
            tx.Rollback()
            c.JSON(http.StatusInternalServerError, gin.H{"error": "Create stage failed: " + err.Error()})
            return
        }
    }

    if err := tx.Commit().Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Transaction commit failed: " + err.Error()})
        return
    }

    config.DB.Preload("Stages").Preload("Vehicles").First(&route, route.ID) // Preload vehicles for full response
    c.JSON(http.StatusCreated, gin.H{"route": toRouteResponse(route)}) // Map to RouteResponse for JSON output
}

// AddStagesToRoute allows adding or replacing stages for an existing route.
func AddStagesToRoute(c *gin.Context) {
    saccoID := uint(c.MustGet("user_id").(float64))
    rID, _ := strconv.ParseUint(c.Param("id"), 10, 64)

    var route models.Route
    if err := config.DB.Where("id=? AND sacco_id=?", rID, saccoID).First(&route).Error; err != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "Route not found"})
        return
    }

    var input struct{ Stages []models.Stage `json:"stages" binding:"required"` }
    if err := c.ShouldBindJSON(&input); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    tx := config.DB.Begin()
    tx.Where("route_id=?", route.ID).Delete(&models.Stage{})
    for i := range input.Stages {
        input.Stages[i].RouteID = route.ID
    }
    tx.Create(&input.Stages)
    tx.Commit()

    config.DB.Preload("Stages").Preload("Vehicles").First(&route, route.ID) // Preload vehicles
    c.JSON(http.StatusOK, gin.H{"route": toRouteResponse(route)}) // Map to RouteResponse for JSON output
}

// ListRoutes returns all routes + stages + vehicles for the authenticated sacco
func ListRoutes(c *gin.Context) {
    authID := uint(c.MustGet("user_id").(float64))
    var user models.User
    config.DB.Preload("Sacco").First(&user, authID)
    if user.Role != "sacco" || user.Sacco == nil {
        c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
        return
    }

    sID := user.Sacco.ID
    var routes []models.Route
    config.DB.Preload("Stages").Preload("Vehicles").Where("sacco_id=?", sID).Find(&routes)

    var routeResponses []RouteResponse
    for _, r := range routes {
        routeResponses = append(routeResponses, toRouteResponse(r)) // Map each route to RouteResponse
    }
    c.JSON(http.StatusOK, gin.H{"routes": routeResponses}) // Return slice of RouteResponse
}

// ListRoutesBySacco fetches routes for a specific sacco (public/admin)
func ListRoutesBySacco(c *gin.Context) {
    sID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
    var routes []models.Route
    config.DB.Preload("Stages").Preload("Vehicles").Where("sacco_id=?", uint(sID)).Find(&routes)

    var routeResponses []RouteResponse
    for _, r := range routes {
        routeResponses = append(routeResponses, toRouteResponse(r)) // Map each route to RouteResponse
    }

    c.JSON(http.StatusOK, gin.H{"routes": routeResponses}) // Return slice of RouteResponse
}

// GetRoute returns a single route + stages + vehicles for the sacco owner
func GetRoute(c *gin.Context) {
    authID := uint(c.MustGet("user_id").(float64))
    rID, _ := strconv.ParseUint(c.Param("id"), 10, 64)

    var route models.Route
    if err := config.DB.Preload("Stages").Preload("Vehicles").Where("id=? AND sacco_id=?", rID, authID).First(&route).Error; err != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "Route not found"})
        return
    }

    c.JSON(http.StatusOK, gin.H{"route": toRouteResponse(route)}) // Map to RouteResponse for JSON output
}

// UpdateRoute handles updating an existing route.
func UpdateRoute(c *gin.Context) {
    authID := uint(c.MustGet("user_id").(float64))
    rID, err := strconv.ParseUint(c.Param("id"), 10, 64)
    if err != nil {
        logrus.WithError(err).Warn("UpdateRoute: Invalid route ID in parameter")
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid route ID"})
        return
    }

    var existingRoute models.Route
    if err := config.DB.First(&existingRoute, rID).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            c.JSON(http.StatusNotFound, gin.H{"error": "Route not found"})
        } else {
            logrus.WithError(err).Error("UpdateRoute: Database error fetching route")
            c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        }
        return
    }

    if err := authorizeRouteUpdate(c, authID, existingRoute.SaccoID); err != nil {
        c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
        return
    }

    var input struct {
        Name        *string `json:"name"`
        Description *string `json:"description"`
        Geometry    *string `json:"geometry"` // Input is still GeoJSON string
    }
    if err := c.ShouldBindJSON(&input); err != nil {
        logrus.WithError(err).Warn("UpdateRoute: Invalid input payload")
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    // Apply updates from input
    if err := applyRouteUpdates(&existingRoute, &input); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    if err := config.DB.Save(&existingRoute).Error; err != nil {
        logrus.WithError(err).Error("UpdateRoute: Failed to save updated route")
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Update failed: " + err.Error()})
        return
    }

    c.JSON(http.StatusOK, gin.H{"route": toRouteResponse(existingRoute)}) // Map to RouteResponse
}

// authorizeRouteUpdate checks if the user is authorized to update the route.
func authorizeRouteUpdate(c *gin.Context, userID, routeSaccoID uint) error {
    var saccoUser models.User
    if err := config.DB.Preload("Sacco").First(&saccoUser, userID).Error; err != nil {
        logrus.WithError(err).Warn("authorizeRouteUpdate: User not found or unauthorized")
        return errors.New("User not authorized")
    }
    if saccoUser.Role != "sacco" || saccoUser.Sacco == nil || saccoUser.Sacco.ID != routeSaccoID {
        return errors.New("Only sacco owner can update this route")
    }
    return nil
}

// applyRouteUpdates updates the route fields based on the input.
func applyRouteUpdates(route *models.Route, input *struct {
    Name        *string `json:"name"`
    Description *string `json:"description"`
    Geometry    *string `json:"geometry"`
}) error {
    if input.Name != nil {
        route.Name = *input.Name
    }
    if input.Description != nil {
        route.Description = *input.Description
    }
    if input.Geometry != nil {
        if *input.Geometry == "" {
            route.Geometry = nil // Set to nil for empty geometry
        } else {
            wkbGeom, err := parseAndConvertGeometry(*input.Geometry)
            if err != nil {
                return errors.New("Invalid geometry: " + err.Error())
            }
            route.Geometry = wkbGeom // Assign []byte directly
        }
    }
    return nil
}

// DeleteRoute removes a route and its stages.
func DeleteRoute(c *gin.Context) {
    authID := uint(c.MustGet("user_id").(float64))
    rID, err := strconv.ParseUint(c.Param("id"), 10, 64)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid route ID"})
        return
    }

    var route models.Route
    if err := config.DB.First(&route, rID).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            c.JSON(http.StatusNotFound, gin.H{"error": "Route not found"})
        } else {
            c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        }
        return
    }

    var saccoUser models.User
    if err := config.DB.Preload("Sacco").First(&saccoUser, authID).Error; err != nil {
        c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authorized"})
        return
    }
    if saccoUser.Role != "sacco" || saccoUser.Sacco.ID != route.SaccoID {
        c.JSON(http.StatusForbidden, gin.H{"error": "Only sacco owner can delete this route"})
        return
    }

    tx := config.DB.Begin()
    if tx.Error != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
        return
    }

    if err := tx.Where("route_id = ?", route.ID).Delete(&models.Stage{}).Error; err != nil {
        tx.Rollback()
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete stages: " + err.Error()})
        return
    }

    if err := tx.Where("id = ? AND sacco_id = ?", route.ID, saccoUser.Sacco.ID).Delete(&models.Route{}).Error; err != nil {
        tx.Rollback()
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete route: " + err.Error()})
        return
    }

    if err := tx.Commit().Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Transaction commit failed: " + err.Error()})
        return
    }

    c.JSON(http.StatusOK, gin.H{"message": "Route deleted successfully"})
}
