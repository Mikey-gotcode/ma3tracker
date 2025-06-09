package controllers

import (
	"fmt" // Import fmt for error formatting
	"net/http"
	"strconv" // Import strconv for parsing route ID

	"github.com/gin-gonic/gin"
	"ma3_tracker/internal/config"
	"ma3_tracker/internal/models"
)

// CreateRoute allows a sacco to create a new route with a GeoJSON LineString.
// Stages are no longer part of initial creation.
func CreateRoute(c *gin.Context) {
	saccoID := uint(c.MustGet("user_id").(float64))

	var input struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		Geometry    string `json:"geometry" binding:"required"` // GeoJSON LineString
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	route := models.Route{
		Name:        input.Name,
		Description: input.Description,
		SaccoID:     saccoID,
		Geometry:    input.Geometry,
		// Stages are intentionally omitted here
	}

	if err := config.DB.Create(&route).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create route: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Route created successfully. You can now add stages to it.", "route": route})
}

// AddStagesToRoute allows adding or replacing stages for an existing route.
// This is a PATCH endpoint for stages.
func AddStagesToRoute(c *gin.Context) {
	saccoID := uint(c.MustGet("user_id").(float64))
	routeIDStr := c.Param("id")
	routeID, err := strconv.ParseUint(routeIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid route ID"})
		return
	}

	var route models.Route
	// Ensure the route exists and belongs to the authenticated sacco
	if err := config.DB.Where("id = ? AND sacco_id = ?", routeID, saccoID).First(&route).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Route not found or unauthorized"})
		return
	}

	var input struct {
		Stages []models.Stage `json:"stages" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Delete existing stages for this route first to allow for a full replacement
	if err := config.DB.Where("route_id = ?", route.ID).Delete(&models.Stage{}).Error; err != nil {
		// Log error but proceed if deletion is not critical (e.g., no stages exist)
		fmt.Printf("Warning: Could not delete existing stages for route %d: %v\n", route.ID, err)
	}

	// Assign RouteID to each new stage and create them
	for i := range input.Stages {
		input.Stages[i].RouteID = route.ID
	}

	if err := config.DB.Create(&input.Stages).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not add stages: " + err.Error()})
		return
	}

	// Reload the route with the newly added stages
	if err := config.DB.Preload("Stages").First(&route, route.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not reload route with stages: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Stages added successfully.", "route": route})
}


// GetRoute retrieves a single route by ID and returns its length in meters
func GetRoute(c *gin.Context) {
	saccoID := uint(c.MustGet("user_id").(float64))
	id := c.Param("id")
	var route models.Route
	if err := config.DB.Preload("Stages").Preload("Vehicles").Where("id = ? AND sacco_id = ?", id, saccoID).First(&route).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Route not found"})
		return
	}

	// Calculate length using PostGIS ST_Length
	var length float64
	err := config.DB.Raw(
		"SELECT ST_Length(geometry::geography) FROM routes WHERE id = ?", id,
	).Scan(&length).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to compute length"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"route":  route,
		"length": length, // meters
	})
}

// ListRoutes returns all routes for the authenticated sacco
func ListRoutes(c *gin.Context) {
	saccoID := uint(c.MustGet("user_id").(float64))
	var routes []models.Route
	if err := config.DB.Preload("Stages").Where("sacco_id = ?", saccoID).Find(&routes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not list routes"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"routes": routes})
}

// SearchRoutesByName finds routes with names matching the query
func SearchRoutesByName(c *gin.Context) {
	saccoID := uint(c.MustGet("user_id").(float64))
	name := c.Query("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name query parameter required"})
		return
	}

	var routes []models.Route
	if err := config.DB.Preload("Stages").Where("sacco_id = ? AND name ILIKE ?", saccoID, "%"+name+"%").Find(&routes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Search failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"routes": routes})
}

// SearchRoutesByStage finds routes containing a given stage name
func SearchRoutesByStage(c *gin.Context) {
	saccoID := uint(c.MustGet("user_id").(float64))
	stageName := c.Query("stage")
	if stageName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Stage query parameter required"})
		return
	}

	var routes []models.Route
	if err := config.DB.
		Preload("Stages").
		Joins("JOIN stages ON stages.route_id = routes.id").
		Where("routes.sacco_id = ? AND stages.name ILIKE ?", saccoID, "%"+stageName+"%").
		Find(&routes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Stage search failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"routes": routes})
}

// UpdateRoute allows modifying route metadata (name, description, geometry).
// It no longer handles stages. Stages are managed via AddStagesToRoute.
func UpdateRoute(c *gin.Context) {
	saccoID := uint(c.MustGet("user_id").(float64))
	id := c.Param("id")
	var route models.Route
	if err := config.DB.Where("id = ? AND sacco_id = ?", id, saccoID).First(&route).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Route not found"})
		return
	}

	var input struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Geometry    *string `json:"geometry"`
		// Stages field removed from here
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if input.Name != nil {
		route.Name = *input.Name
	}
	if input.Description != nil {
		route.Description = *input.Description
	}
	if input.Geometry != nil {
		route.Geometry = *input.Geometry
	}

	if err := config.DB.Save(&route).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Update failed"})
		return
	}
	// Reload with stages if you want to return the full route representation
	config.DB.Preload("Stages").First(&route, route.ID)
	c.JSON(http.StatusOK, gin.H{"route": route})
}

// DeleteRoute removes a route and its stages
func DeleteRoute(c *gin.Context) {
	saccoID := uint(c.MustGet("user_id").(float64))
	id := c.Param("id")

	// Start a transaction to ensure atomicity
	tx := config.DB.Begin()
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
		return
	}

	// delete stages first
	if err := tx.Where("route_id = ?", id).Delete(&models.Stage{}).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete route stages: " + err.Error()})
		return
	}

	// then delete route
	if err := tx.Where("id = ? AND sacco_id = ?", id, saccoID).Delete(&models.Route{}).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Route deletion failed: " + err.Error()})
		return
	}

	tx.Commit()
	c.JSON(http.StatusOK, gin.H{"message": "Route and its stages deleted successfully."})
}