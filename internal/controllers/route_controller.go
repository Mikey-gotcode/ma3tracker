package controllers

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"ma3_tracker/internal/config"
	"ma3_tracker/internal/models"

	"database/sql"

	"github.com/twpayne/go-geom"
	gjson "github.com/twpayne/go-geom/encoding/geojson"
	"github.com/twpayne/go-geom/encoding/wkb"
)

// RouteResponse struct for API output (for Sacco owners)
type RouteResponse struct {
	ID          uint           `json:"ID"`
	CreatedAt   time.Time      `json:"CreatedAt"`
	UpdatedAt   time.Time      `json:"UpdatedAt"`
	DeletedAt   gorm.DeletedAt `json:"DeletedAt,omitempty"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	SaccoID     uint           `json:"sacco_id"`
	Geometry    string         `json:"geometry"`
	Stages      []models.Stage `json:"stages"`
	Vehicles    []models.Vehicle `json:"vehicles"`
}

// CommuterRouteResponse is the structure sent back to the Flutter app for an optimal route
type CommuterRouteResponse struct {
	ID          uint                 `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Geometry    json.RawMessage      `json:"geometry"`
	Stages      []RouteStageResponse `json:"stages,omitempty"`
	IsComposite bool                 `json:"is_composite"`
}

// RouteStageResponse represents a segment of a composite route returned to the commuter
type RouteStageResponse struct {
	RouteID     uint            `json:"route_id"`
	RouteName   string          `json:"route_name"`
	Description string          `json:"description"`
	Geometry    json.RawMessage `json:"geometry"`
}

// FindRouteRequest includes details for route search
type FindRouteRequest struct {
	StartLat              float64 `json:"start_lat" binding:"required"`
	StartLon              float64 `json:"start_lon" binding:"required"`
	EndLat                float64 `json:"end_lat" binding:"required"`
	EndLon                float64 `json:"end_lon" binding:"required"`
	OptimalGeometryGeoJSON string  `json:"optimal_geometry_geojson" binding:"required"`
}

// toRouteResponse converts a models.Route to a RouteResponse
func toRouteResponse(route models.Route) RouteResponse {
	jsonGeom, _ := convertWKBToGeoJSON(route.Geometry)
	return RouteResponse{
		ID:          route.ID,
		CreatedAt:   route.CreatedAt,
		UpdatedAt:   route.UpdatedAt,
		DeletedAt:   route.DeletedAt,
		Name:        route.Name,
		Description: route.Description,
		SaccoID:     route.SaccoID,
		Geometry:    jsonGeom,
		Stages:      route.Stages,
		Vehicles:    route.Vehicles,
	}
}

// parseAndConvertGeometry parses a GeoJSON string into a geom.T and returns WKB bytes
func parseAndConvertGeometry(rawGeoJSON string) ([]byte, error) {
	if rawGeoJSON == "" {
		logrus.Debug("parseAndConvertGeometry: Empty raw GeoJSON string provided.")
		return nil, nil
	}
	var g geom.T
	err := gjson.Unmarshal([]byte(rawGeoJSON), &g)
	if err != nil {
		logrus.WithError(err).Error("parseAndConvertGeometry: Failed to unmarshal GeoJSON.")
		return nil, fmt.Errorf("failed to unmarshal GeoJSON: %w", err)
	}

	wkbBytes, err := wkb.Marshal(g, binary.LittleEndian)
	if err != nil {
		logrus.WithError(err).Error("parseAndConvertGeometry: Failed to marshal geometry to WKB.")
		return nil, fmt.Errorf("failed to marshal geometry to WKB: %w", err)
	}
	return wkbBytes, nil
}

// convertWKBToGeoJSON converts WKB bytes into a GeoJSON string
func convertWKBToGeoJSON(wkbBytes []byte) (string, error) {
	if len(wkbBytes) == 0 {
		logrus.Debug("convertWKBToGeoJSON: Empty WKB bytes provided, returning empty string.")
		return "", nil
	}
	g, err := wkb.Unmarshal(wkbBytes)
	if err != nil {
		logrus.WithError(err).Error("convertWKBToGeoJSON: Failed to unmarshal WKB.")
		return "", fmt.Errorf("failed to unmarshal WKB: %w", err)
	}
	b, err := gjson.Marshal(g)
	if err != nil {
		logrus.WithError(err).Error("convertWKBToGeoJSON: Failed to marshal geometry to GeoJSON bytes.")
		return "", fmt.Errorf("failed to marshal geometry to GeoJSON: %w", err)
	}
	return string(b), nil
}

// findDirectMatchingRoute attempts to find a single existing route closely matching the ORS path.
// findDirectMatchingRoute attempts to find a single existing route closely matching the ORS path.
func findDirectMatchingRoute(orsWKBGeometry []byte) (*CommuterRouteResponse, error) {
	logrus.Info("findDirectMatchingRoute: Attempting to find a direct matching route.")

	const endpointTolerance = 0.0005 // Approx 50 meters
	query := `
		SELECT
			r.id, r.name, r.description, ST_AsGeoJSON(r.geometry::geometry) AS geometry_geojson
		FROM
			routes r, ST_GeomFromWKB($1, 4326) AS ors_geom
		WHERE
			ST_Intersects(ST_SetSRID(r.geometry::geometry, 4326), ors_geom) AND -- Explicitly set SRID for r.geometry
			ST_DWithin(ST_SetSRID(ST_StartPoint(r.geometry), 4326), ST_StartPoint(ors_geom), $2) AND -- Explicitly set SRID
			ST_DWithin(ST_SetSRID(ST_EndPoint(r.geometry), 4326), ST_EndPoint(ors_geom), $2) -- Explicitly set SRID
		ORDER BY
			ST_Length(ST_Intersection(ST_SetSRID(r.geometry::geometry, 4326), ors_geom)) DESC, -- Explicitly set SRID
			ST_HausdorffDistance(ST_SetSRID(r.geometry::geometry, 4326), ors_geom) ASC -- Explicitly set SRID
		LIMIT 1;
	`
	row := config.DB.Raw(query, orsWKBGeometry, endpointTolerance).Row()

	var (
		id          uint
		name        string
		description sql.NullString
		geometryGeoJSON []byte
	)

	err := row.Scan(&id, &name, &description, &geometryGeoJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			logrus.Info("findDirectMatchingRoute: No direct matching route found.")
			return nil, nil
		}
		logrus.WithError(err).Error("findDirectMatchingRoute: Database error scanning direct route result.")
		return nil, fmt.Errorf("database error scanning direct route: %w", err)
	}

	logrus.Infof("findDirectMatchingRoute: Found a direct matching route (ID: %d).", id)
	return &CommuterRouteResponse{
		ID:          id,
		Name:        name,
		Description: description.String,
		Geometry:    json.RawMessage(geometryGeoJSON),
		IsComposite: false,
	}, nil
}

// findCompositeRouteCandidates finds existing routes that significantly intersect the ORS path.
func findCompositeRouteCandidates(orsWKBGeometry []byte) ([]RouteStageResponse, error) {
	logrus.Info("findCompositeRouteCandidates: Attempting to find relevant routes for composite search.")

	const intersectionLengthThreshold = 0.001 // Minimum intersection length to consider a segment relevant
	query := `
		SELECT
			r.id, r.name, r.description, ST_AsGeoJSON(r.geometry::geometry) AS geometry_geojson,
			ST_Length(ST_Intersection(ST_SetSRID(r.geometry::geometry, 4326), ST_GeomFromWKB($1, 4326))) AS intersection_length -- Explicitly set SRID
		FROM
			routes r
		WHERE
			ST_Intersects(ST_SetSRID(r.geometry::geometry, 4326), ST_GeomFromWKB($1, 4326)) -- Explicitly set SRID
		ORDER BY
			intersection_length DESC
		LIMIT 5;
	`
	rows, err := config.DB.Raw(query, orsWKBGeometry).Rows()
	if err != nil {
		logrus.WithError(err).Error("findCompositeRouteCandidates: Database error executing segment match query.")
		return nil, fmt.Errorf("database error executing segment match query: %w", err)
	}
	defer rows.Close()

	var candidates []RouteStageResponse
	for rows.Next() {
		var (
			routeID             uint
			routeName           string
			routeDescription    sql.NullString
			routeGeometryGeoJSON []byte
			intersectionLength  float64
		)
		err = rows.Scan(&routeID, &routeName, &routeDescription, &routeGeometryGeoJSON, &intersectionLength)
		if err != nil {
			logrus.WithError(err).Warn("findCompositeRouteCandidates: Error scanning candidate row. Skipping.")
			continue
		}

		if intersectionLength < intersectionLengthThreshold {
			continue
		}

		candidates = append(candidates, RouteStageResponse{
			RouteID:     routeID,
			RouteName:   routeName,
			Description: routeDescription.String,
			Geometry:    json.RawMessage(routeGeometryGeoJSON),
		})
	}
	if err = rows.Err(); err != nil {
		logrus.WithError(err).Error("findCompositeRouteCandidates: Error after iterating composite candidate rows.")
		return nil, fmt.Errorf("error after iterating composite candidate rows: %w", err)
	}
	return candidates, nil
}

// FindOptimalRoute handles finding the best route between two points for commuters,
// leveraging the frontend-provided optimal_geometry_geojson.
func FindOptimalRoute(c *gin.Context) {
	logrus.Info("FindOptimalRoute: Starting route optimization process.")
	var req FindRouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logrus.WithError(err).Warn("FindOptimalRoute: Invalid request body or missing optimal_geometry_geojson.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body or missing optimal_geometry_geojson: " + err.Error()})
		return
	}

	logrus.WithFields(logrus.Fields{
		"start_lat": req.StartLat,
		"start_lon": req.StartLon,
		"end_lat":   req.EndLat,
		"end_lon":   req.EndLon,
		"ors_geojson_len": len(req.OptimalGeometryGeoJSON),
	}).Info("FindOptimalRoute: Received request with ORS generated geometry.")

	orsWKBGeometry, err := parseAndConvertGeometry(req.OptimalGeometryGeoJSON)
	if err != nil {
		logrus.WithError(err).Error("FindOptimalRoute: Failed to parse optimal_geometry_geojson.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid optimal_geometry_geojson: " + err.Error()})
		return
	}

	// Step 1: Attempt to find a direct single route match
	directRoute, err := findDirectMatchingRoute(orsWKBGeometry)
	if err != nil {
		logrus.WithError(err).Error("FindOptimalRoute: Error searching for direct route.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to find any route due to backend error: " + err.Error()})
		return
	}
	if directRoute != nil {
		c.JSON(http.StatusOK, gin.H{"data": []CommuterRouteResponse{*directRoute}})
		return
	}

	// Step 2: If no direct match, attempt to find composite route candidates
	compositeCandidates, err := findCompositeRouteCandidates(orsWKBGeometry)
	if err != nil {
		logrus.WithError(err).Error("FindOptimalRoute: Error searching for composite candidates.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to find any route due to backend error: " + err.Error()})
		return
	}

	if len(compositeCandidates) > 0 {
		logrus.Infof("FindOptimalRoute: Found %d composite route candidates. Responding.", len(compositeCandidates))
		c.JSON(http.StatusOK, gin.H{"data": []CommuterRouteResponse{
			{
				ID:          0, // No single ID for composite
				Name:        "Composite Route",
				Description: "Generated from multiple segments matching optimal path",
				Geometry:    json.RawMessage(req.OptimalGeometryGeoJSON), // Use ORS geometry as the overall composite path
				Stages:      compositeCandidates,
				IsComposite: true,
			},
		}})
		return
	}

	logrus.Info("FindOptimalRoute: No direct or significant composite routes found.")
	c.JSON(http.StatusNotFound, gin.H{"error": "No existing routes found that closely match the requested path."})
}

// CreateRoute allows a sacco to create a new route with GeoJSON LineString and stages.
func CreateRoute(c *gin.Context) {
	logrus.Info("CreateRoute: Handling new route creation request.")
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
		logrus.WithError(err).Warn("CreateRoute: Invalid input payload.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input: " + err.Error()})
		return
	}
	logrus.Debugf("CreateRoute: Input received for route '%s'.", input.Name)

	authenticatedUserID := uint(c.MustGet("user_id").(float64))
	var saccoUser models.User
	if err := config.DB.Preload("Sacco").First(&saccoUser, authenticatedUserID).Error; err != nil {
		logrus.WithError(err).WithField("user_id", authenticatedUserID).Error("CreateRoute: User not found or unauthorized.")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authorized"})
		return
	}
	if saccoUser.Role != "sacco" || saccoUser.Sacco == nil {
		logrus.WithField("user_id", authenticatedUserID).Warn("CreateRoute: User is not a sacco owner or has no associated sacco.")
		c.JSON(http.StatusForbidden, gin.H{"error": "Only sacco owners can create routes"})
		return
	}
	saccoID := saccoUser.Sacco.ID
	logrus.Debugf("CreateRoute: Authenticated sacco user '%s' (ID: %d) found.", saccoID)

	tx := config.DB.Begin()
	if tx.Error != nil {
		logrus.WithError(tx.Error).Error("CreateRoute: Failed to start database transaction.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
		return
	}
	logrus.Debug("CreateRoute: Database transaction started.")

	wkbGeom, err := parseAndConvertGeometry(input.Geometry)
	if err != nil {
		tx.Rollback()
		logrus.WithError(err).Error("CreateRoute: Invalid geometry provided.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid geometry: " + err.Error()})
		return
	}
	logrus.Debug("CreateRoute: Geometry parsed and converted to WKB.")

	route := models.Route{Name: input.Name, Description: input.Description, SaccoID: saccoID, Geometry: wkbGeom}
	if err := tx.Create(&route).Error; err != nil {
		tx.Rollback()
		logrus.WithError(err).Error("CreateRoute: Failed to create route record.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Create route failed: " + err.Error()})
		return
	}
	logrus.Debugf("CreateRoute: Route '%s' (ID: %d) created.", route.Name, route.ID)


	for _, s := range input.Stages {
		stage := models.Stage{Name: s.Name, Seq: s.Seq, Lat: s.Lat, Lng: s.Lng, RouteID: route.ID}
		if err := tx.Create(&stage).Error; err != nil {
			tx.Rollback()
			logrus.WithError(err).WithField("stage_name", s.Name).Error("CreateRoute: Failed to create stage record.")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Create stage failed: " + err.Error()})
			return
		}
		logrus.Debugf("CreateRoute: Stage '%s' for route %d created.", stage.Name, route.ID)
	}

	if err := tx.Commit().Error; err != nil {
		logrus.WithError(err).Error("CreateRoute: Database transaction commit failed.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Transaction commit failed: " + err.Error()})
		return
	}
	logrus.Info("CreateRoute: Route and stages created successfully.")

	config.DB.Preload("Stages").Preload("Vehicles").First(&route, route.ID)
	c.JSON(http.StatusCreated, gin.H{"data": toRouteResponse(route)})
}

// AddStagesToRoute allows adding or replacing stages for an existing route.
func AddStagesToRoute(c *gin.Context) {
	logrus.Info("AddStagesToRoute: Handling add/replace stages request.")
	authID := uint(c.MustGet("user_id").(float64))
	rID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		logrus.WithError(err).Warn("AddStagesToRoute: Invalid route ID in parameter.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid route ID"})
		return
	}
	logrus.WithField("route_id", rID).Debug("AddStagesToRoute: Processing request for route.")

	var route models.Route
	if err := config.DB.Where("id=?", rID).First(&route).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			logrus.WithField("route_id", rID).Warn("AddStagesToRoute: Route not found.")
			c.JSON(http.StatusNotFound, gin.H{"error": "Route not found"})
		} else {
			logrus.WithError(err).WithField("route_id", rID).Error("AddStagesToRoute: Database error fetching route.")
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	logrus.Debugf("AddStagesToRoute: Route '%s' (ID: %d) found.", route.Name, route.ID)

	var saccoUser models.User
	if err := config.DB.Preload("Sacco").First(&saccoUser, authID).Error; err != nil {
		logrus.WithError(err).WithField("user_id", authID).Error("AddStagesToRoute: User not found or unauthorized.")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authorized"})
		return
	}
	if saccoUser.Role != "sacco" || saccoUser.Sacco == nil || saccoUser.Sacco.ID != route.SaccoID {
		logrus.WithFields(logrus.Fields{
			"user_id": authID,
			"route_sacco_id": route.SaccoID,
			"user_sacco_id": saccoUser.Sacco.ID,
		}).Warn("AddStagesToRoute: User not authorized to modify this route.")
		c.JSON(http.StatusForbidden, gin.H{"error": "Only sacco owner can modify this route"})
		return
	}
	logrus.Debug("AddStagesToRoute: User authorized to modify route.")

	var input struct{ Stages []models.Stage `json:"stages" binding:"required"` }
	if err := c.ShouldBindJSON(&input); err != nil {
		logrus.WithError(err).Warn("AddStagesToRoute: Invalid input payload for stages.")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	logrus.Debugf("AddStagesToRoute: Received %d stages in input.", len(input.Stages))


	tx := config.DB.Begin()
	if tx.Error != nil {
		logrus.WithError(tx.Error).Error("AddStagesToRoute: Failed to start database transaction.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
		return
	}
	logrus.Debug("AddStagesToRoute: Database transaction started.")

	if err := tx.Where("route_id=?", route.ID).Delete(&models.Stage{}).Error; err != nil {
		tx.Rollback()
		logrus.WithError(err).WithField("route_id", route.ID).Error("AddStagesToRoute: Failed to delete existing stages.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete existing stages"})
		return
	}
	logrus.Debugf("AddStagesToRoute: Existing stages for route %d deleted.", route.ID)


	for i := range input.Stages {
		input.Stages[i].RouteID = route.ID
	}
	if err := tx.Create(&input.Stages).Error; err != nil {
		tx.Rollback()
		logrus.WithError(err).WithField("route_id", route.ID).Error("AddStagesToRoute: Failed to add new stages.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add new stages"})
		return
	}
	logrus.Debugf("AddStagesToRoute: New stages for route %d added.", route.ID)

	if err := tx.Commit().Error; err != nil {
		logrus.WithError(err).Error("AddStagesToRoute: Database transaction commit failed.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Transaction commit failed: " + err.Error()})
		return
	}
	logrus.Info("AddStagesToRoute: Stages added/replaced successfully.")

	config.DB.Preload("Stages").Preload("Vehicles").First(&route, route.ID)
	c.JSON(http.StatusOK, gin.H{"data": toRouteResponse(route)})
}

// ListRoutes returns all routes + stages + vehicles for the authenticated sacco
// This method is specifically for sacco users to view THEIR routes.
func ListRoutes(c *gin.Context) {
	logrus.Info("ListRoutes: Handling list routes request for authenticated sacco.")
	authID := uint(c.MustGet("user_id").(float64))
	var user models.User
	if err := config.DB.Preload("Sacco").First(&user, authID).Error; err != nil {
		logrus.WithError(err).WithField("user_id", authID).Error("ListRoutes: User not found or failed to preload sacco.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "User data error"}) // Or Unauthorized if it means the user isn't authenticated properly
		return
	}

	if user.Role != "sacco" || user.Sacco == nil {
		logrus.WithField("user_id", authID).Warn("ListRoutes: User is not a sacco or has no associated sacco.")
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	sID := user.Sacco.ID
	logrus.Debugf("ListRoutes: Fetching routes for Sacco ID: %d", sID)
	var routes []models.Route
	if err := config.DB.Preload("Stages").Preload("Vehicles").Where("sacco_id=?", sID).Find(&routes).Error; err != nil {
		logrus.WithError(err).WithField("sacco_id", sID).Error("ListRoutes: Database error fetching routes for sacco.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch routes"})
		return
	}

	var routeResponses []RouteResponse
	for _, r := range routes {
		routeResponses = append(routeResponses, toRouteResponse(r))
	}
	logrus.Infof("ListRoutes: Found %d routes for Sacco ID %d.", len(routeResponses), sID)
	c.JSON(http.StatusOK, gin.H{"data": routeResponses})
}

// ListAllCommuterRoutes returns all routes + stages + vehicles for the commuter.
// This method does NOT filter by sacco_id and does NOT check for 'sacco' role.
// It is intended for public/commuter-facing route data.
func ListAllCommuterRoutes(c *gin.Context) {
	logrus.Info("ListAllCommuterRoutes: Handling list all commuter routes request.")
	var routes []models.Route
	if err := config.DB.Preload("Stages").Preload("Vehicles").Find(&routes).Error; err != nil {
		logrus.WithError(err).Error("ListAllCommuterRoutes: Database error fetching all routes.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch routes"})
		return
	}

	var routeResponses []RouteResponse
	for _, r := range routes {
		routeResponses = append(routeResponses, toRouteResponse(r))
	}
	logrus.Infof("ListAllCommuterRoutes: Found %d routes for commuters.", len(routeResponses))
	c.JSON(http.StatusOK, gin.H{"data": routeResponses})
}


// ListRoutesBySacco fetches routes for a specific sacco (public/admin)
// This method might be redundant if ListAllCommuterRoutes covers the public need
// and ListRoutes covers sacco-specific need. Review usage.
func ListRoutesBySacco(c *gin.Context) {
	logrus.Info("ListRoutesBySacco: Handling list routes by specific sacco ID request.")
	sID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		logrus.WithError(err).WithField("sacco_id_param", c.Param("id")).Warn("ListRoutesBySacco: Invalid Sacco ID parameter.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Sacco ID"})
		return
	}
	logrus.Debugf("ListRoutesBySacco: Fetching routes for Sacco ID: %d.", sID)

	var routes []models.Route
	if err := config.DB.Preload("Stages").Preload("Vehicles").Where("sacco_id=?", uint(sID)).Find(&routes).Error; err != nil {
		logrus.WithError(err).WithField("sacco_id", sID).Error("ListRoutesBySacco: Database error fetching routes for specific sacco.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch routes"})
		return
	}

	var routeResponses []RouteResponse
	for _, r := range routes {
		routeResponses = append(routeResponses, toRouteResponse(r))
	}
	logrus.Infof("ListRoutesBySacco: Found %d routes for Sacco ID %d.", len(routeResponses), sID)
	c.JSON(http.StatusOK, gin.H{"data": routeResponses})
}

// GetRoute returns a single route + stages + vehicles for the sacco owner
func GetRoute(c *gin.Context) {
	logrus.Info("GetRoute: Handling get single route request for sacco owner.")
	authID := uint(c.MustGet("user_id").(float64))
	rID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		logrus.WithError(err).Warn("GetRoute: Invalid route ID in parameter.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid route ID"})
		return
	}
	logrus.WithFields(logrus.Fields{"user_id": authID, "route_id": rID}).Debug("GetRoute: Processing request.")

	var route models.Route
	if err := config.DB.Preload("Stages").Preload("Vehicles").Where("id=?", rID).First(&route).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			logrus.WithField("route_id", rID).Warn("GetRoute: Route not found in database.")
			c.JSON(http.StatusNotFound, gin.H{"error": "Route not found"})
		} else {
			logrus.WithError(err).WithField("route_id", rID).Error("GetRoute: Database error fetching route.")
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	logrus.Debugf("GetRoute: Route '%s' (ID: %d) found.", route.Name, route.ID)


	var saccoUser models.User
	if err := config.DB.Preload("Sacco").First(&saccoUser, authID).Error; err != nil {
		logrus.WithError(err).WithField("user_id", authID).Error("GetRoute: User not found or unauthorized.")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authorized"})
		return
	}
	if saccoUser.Role != "sacco" || saccoUser.Sacco == nil || saccoUser.Sacco.ID != route.SaccoID {
		logrus.WithFields(logrus.Fields{
			"user_id": authID,
			"route_sacco_id": route.SaccoID,
			"user_sacco_id": saccoUser.Sacco.ID,
		}).Warn("GetRoute: User not authorized to view this route.")
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied: Route does not belong to this sacco"})
		return
	}
	logrus.Info("GetRoute: Route successfully retrieved and authorized.")
	c.JSON(http.StatusOK, gin.H{"data": toRouteResponse(route)})
}

// UpdateRoute handles updating an existing route.
func UpdateRoute(c *gin.Context) {
	logrus.Info("UpdateRoute: Handling route update request.")
	authID := uint(c.MustGet("user_id").(float64))
	rID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		logrus.WithError(err).Warn("UpdateRoute: Invalid route ID in parameter.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid route ID"})
		return
	}
	logrus.WithFields(logrus.Fields{"user_id": authID, "route_id": rID}).Debug("UpdateRoute: Processing request.")

	var existingRoute models.Route
	if err := config.DB.First(&existingRoute, rID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			logrus.WithField("route_id", rID).Warn("UpdateRoute: Route not found in database.")
			c.JSON(http.StatusNotFound, gin.H{"error": "Route not found"})
		} else {
			logrus.WithError(err).WithField("route_id", rID).Error("UpdateRoute: Database error fetching route for update.")
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	logrus.Debugf("UpdateRoute: Existing route '%s' (ID: %d) found.", existingRoute.Name, existingRoute.ID)

	var saccoUser models.User
	if err := config.DB.Preload("Sacco").First(&saccoUser, authID).Error; err != nil {
		logrus.WithError(err).WithField("user_id", authID).Warn("UpdateRoute: User not found or unauthorized.")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authorized"})
		return
	}
	if saccoUser.Role != "sacco" || saccoUser.Sacco == nil || saccoUser.Sacco.ID != existingRoute.SaccoID {
		logrus.WithFields(logrus.Fields{
			"user_id": authID,
			"route_sacco_id": existingRoute.SaccoID,
			"user_sacco_id": saccoUser.Sacco.ID,
		}).Warn("UpdateRoute: User not authorized to update this route.")
		c.JSON(http.StatusForbidden, gin.H{"error": "Only sacco owner can update this route"})
		return
	}
	logrus.Debug("UpdateRoute: User authorized to update route.")

	var input struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Geometry    *string `json:"geometry"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		logrus.WithError(err).Warn("UpdateRoute: Invalid input payload for update.")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	logrus.Debug("UpdateRoute: Input payload for update parsed.")

	if input.Name != nil {
		existingRoute.Name = *input.Name
		logrus.Debugf("UpdateRoute: Updating name to '%s'.", *input.Name)
	}
	if input.Description != nil {
		existingRoute.Description = *input.Description
		logrus.Debugf("UpdateRoute: Updating description to '%s'.", *input.Description)
	}
	if input.Geometry != nil {
		if *input.Geometry == "" {
			existingRoute.Geometry = nil
			logrus.Debug("UpdateRoute: Setting geometry to nil (empty string input).")
		} else {
			wkbGeom, err := parseAndConvertGeometry(*input.Geometry)
			if err != nil {
				logrus.WithError(err).Error("UpdateRoute: Invalid geometry provided for update.")
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid geometry: " + err.Error()})
				return
			}
			existingRoute.Geometry = wkbGeom
			logrus.Debug("UpdateRoute: Geometry updated and converted to WKB.")
		}
	}

	if err := config.DB.Save(&existingRoute).Error; err != nil {
		logrus.WithError(err).Error("UpdateRoute: Failed to save updated route to database.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Update failed: " + err.Error()})
		return
	}
	logrus.Info("UpdateRoute: Route updated successfully.")

	config.DB.Preload("Stages").Preload("Vehicles").First(&existingRoute, existingRoute.ID)
	c.JSON(http.StatusOK, gin.H{"data": toRouteResponse(existingRoute)})
}

// DeleteRoute removes a route and its stages.
func DeleteRoute(c *gin.Context) {
	logrus.Info("DeleteRoute: Handling route deletion request.")
	authID := uint(c.MustGet("user_id").(float64))
	rID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		logrus.WithError(err).Warn("DeleteRoute: Invalid route ID in parameter.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid route ID"})
		return
	}
	logrus.WithFields(logrus.Fields{"user_id": authID, "route_id": rID}).Debug("DeleteRoute: Processing request.")

	var route models.Route
	if err := config.DB.First(&route, rID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			logrus.WithField("route_id", rID).Warn("DeleteRoute: Route not found in database.")
			c.JSON(http.StatusNotFound, gin.H{"error": "Route not found"})
		} else {
			logrus.WithError(err).WithField("route_id", rID).Error("DeleteRoute: Database error fetching route for deletion.")
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	logrus.Debugf("DeleteRoute: Route '%s' (ID: %d) found.", route.Name, route.ID)


	var saccoUser models.User
	if err := config.DB.Preload("Sacco").First(&saccoUser, authID).Error; err != nil {
		logrus.WithError(err).WithField("user_id", authID).Error("DeleteRoute: User not found or unauthorized.")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authorized"})
		return
	}
	if saccoUser.Role != "sacco" || saccoUser.Sacco.ID != route.SaccoID {
		logrus.WithFields(logrus.Fields{
			"user_id": authID,
			"route_sacco_id": route.SaccoID,
			"user_sacco_id": saccoUser.Sacco.ID,
		}).Warn("DeleteRoute: User not authorized to delete this route.")
		c.JSON(http.StatusForbidden, gin.H{"error": "Only sacco owner can delete this route"})
		return
	}
	logrus.Debug("DeleteRoute: User authorized to delete route.")

	tx := config.DB.Begin()
	if tx.Error != nil {
		logrus.WithError(tx.Error).Error("DeleteRoute: Failed to start database transaction for deletion.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
		return
	}
	logrus.Debug("DeleteRoute: Database transaction started.")

	if err := tx.Where("route_id = ?", route.ID).Delete(&models.Stage{}).Error; err != nil {
		tx.Rollback()
		logrus.WithError(err).WithField("route_id", route.ID).Error("DeleteRoute: Failed to delete associated stages.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete stages: " + err.Error()})
		return
	}
	logrus.Debugf("DeleteRoute: Associated stages for route %d deleted.", route.ID)


	if err := tx.Where("id = ? AND sacco_id = ?", route.ID, saccoUser.Sacco.ID).Delete(&models.Route{}).Error; err != nil {
		tx.Rollback()
		logrus.WithError(err).WithField("route_id", route.ID).Error("DeleteRoute: Failed to delete route record.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete route: " + err.Error()})
		return
	}
	logrus.Debugf("DeleteRoute: Route %d record deleted.", route.ID)

	if err := tx.Commit().Error; err != nil {
		logrus.WithError(err).Error("DeleteRoute: Database transaction commit failed.")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Transaction commit failed: " + err.Error()})
		return
	}
	logrus.Info("DeleteRoute: Route and its stages deleted successfully.")

	c.JSON(http.StatusOK, gin.H{"message": "Route deleted successfully"})
}
