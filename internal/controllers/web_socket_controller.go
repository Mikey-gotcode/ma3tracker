package controllers

import (
	// "database/sql" // Removed: No longer directly used after switching to direct Vehicle model query
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"ma3_tracker/internal/config"
	"ma3_tracker/internal/middleware"
	"ma3_tracker/internal/models"
)

// upgrader configures the WebSocket connection.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all for development (restrict in production!)
	},
}

// LocationData struct defines the format of incoming JSON from Flutter (driver's update).
// Timestamp remains time.Time, relying on the custom UnmarshalJSON.
type LocationData struct {
	DriverID  uint      `json:"driver_id"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Accuracy  float64   `json:"accuracy"`  // GPS accuracy in meters
	Speed     float64   `json:"speed"`     // Speed in m/s
	Bearing   float64   `json:"bearing"`   // Direction in degrees
	Altitude  float64   `json:"altitude"`  // Altitude in meters
	Timestamp time.Time `json:"timestamp"` // Handled by custom UnmarshalJSON
}

// UnmarshalJSON implements a custom unmarshaler for LocationData to handle various timestamp formats.
func (ld *LocationData) UnmarshalJSON(data []byte) error {
	// Alias to avoid infinite recursion during unmarshaling.
	type alias LocationData
	aux := &struct {
		Timestamp string `json:"timestamp"`
		*alias
	}{alias: (*alias)(ld)}

	// Unmarshal into the auxiliary struct first to get the timestamp as a string.
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	ts := aux.Timestamp
	// Check if the timestamp string has a timezone suffix (Z for UTC, or +/- offset).
	// If not, append 'Z' to assume UTC, which helps RFC3339Nano parsing.
	if !(strings.HasSuffix(ts, "Z") || strings.ContainsAny(ts[len(ts)-6:], "+-")) {
		ts += "Z"
	}
	
	// Parse with full nanosecond precision using RFC3339Nano, which now expects a 'Z'.
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		// Log specific error for debugging timestamp issues.
		logrus.WithFields(logrus.Fields{
			"raw_timestamp": aux.Timestamp,
			"parsed_string": ts,
			"parse_error":   err,
		}).Error("Custom UnmarshalJSON: failed to parse timestamp.")
		return fmt.Errorf("invalid timestamp %q: %w", aux.Timestamp, err)
	}
	ld.Timestamp = t // Assign the parsed time.Time to the actual struct field.
	return nil
}

// LocationHub manages active WebSocket connections for Sacco monitoring and broadcasts updates.
type LocationHub struct {
	saccoClients map[uint]map[*websocket.Conn]bool
	broadcast    chan map[string]interface{}
	mu           sync.Mutex
}

// NewLocationHub creates and returns a new LocationHub instance.
// It also starts a goroutine to continuously run the broadcasting logic.
func NewLocationHub() *LocationHub {
	hub := &LocationHub{
		saccoClients: make(map[uint]map[*websocket.Conn]bool),
		broadcast:    make(chan map[string]interface{}, 100),
	}
	go hub.run() // Start the goroutine for broadcasting messages
	return hub
}

// run listens for messages on the broadcast channel and sends them to relevant Sacco clients.
func (h *LocationHub) run() {
	for msg := range h.broadcast {
		h.mu.Lock()
		// sacco_id is now explicitly float64 when put into broadcast map,
		// so this type assertion should always succeed if data is present.
		msgSaccoIDFloat, ok := msg["sacco_id"].(float64)
		if !ok {
			logrus.Warn("Broadcast message missing 'sacco_id' or has wrong type (expected float64). Skipping broadcast.")
			h.mu.Unlock()
			continue
		}
		msgSaccoID := uint(msgSaccoIDFloat)

		if clients, exists := h.saccoClients[msgSaccoID]; exists {
			for conn := range clients {
				// FIX: Changed parameter name from 'm' to 'broadcastMessage' to resolve potential undefined issue.
				// Explicitly pass msg into the goroutine to avoid common closure issues.
				go func(c *websocket.Conn, broadcastMessage map[string]interface{}) { 
					err := c.WriteJSON(broadcastMessage) // Use the new parameter name here
					if err != nil {
						if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure, websocket.CloseAbnormalClosure) {
							logrus.WithFields(logrus.Fields{
								"sacco_id": msgSaccoID,
								"conn_ptr": fmt.Sprintf("%p", c),
							}).Info("Client connection closed during broadcast, unregistering.")
							h.UnregisterClient(msgSaccoID, c)
						} else {
							logrus.WithError(err).WithFields(logrus.Fields{
								"sacco_id": msgSaccoID,
								"conn_ptr": fmt.Sprintf("%p", c),
							}).Warn("Failed to send broadcast message to client.")
						}
					}
				}(conn, msg) // Pass 'msg' (the current message from the channel) as 'broadcastMessage'
			}
		}
		h.mu.Unlock()
	}
}

// RegisterClient registers a new Sacco client connection with the hub.
func (h *LocationHub) RegisterClient(saccoID uint, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.saccoClients[saccoID]; !ok {
		h.saccoClients[saccoID] = make(map[*websocket.Conn]bool)
	}
	h.saccoClients[saccoID][conn] = true
	logrus.WithFields(logrus.Fields{
		"sacco_id": saccoID,
		"conn_ptr": fmt.Sprintf("%p", conn),
	}).Info("Client registered with LocationHub (Sacco or Commuter).")
}

// UnregisterClient removes a disconnected Sacco client connection from the hub.
func (h *LocationHub) UnregisterClient(saccoID uint, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if clients, ok := h.saccoClients[saccoID]; ok {
		delete(clients, conn)
		if len(clients) == 0 {
			delete(h.saccoClients, saccoID)
			logrus.WithField("sacco_id", saccoID).Debug("Removed Sacco entry as no clients are left.")
		}
	}
	logrus.WithFields(logrus.Fields{
		"sacco_id": saccoID,
		"conn_ptr": fmt.Sprintf("%p", conn),
	}).Info("Client unregistered from LocationHub (Sacco or Commuter).")
}

// PublishLocation publishes a new location update to the broadcast channel.
func (h *LocationHub) PublishLocation(data map[string]interface{}) {
	select {
	case h.broadcast <- data:
		// Message sent to broadcast channel successfully.
	default:
		logrus.Warn("Location broadcast channel full, dropping message. Consider increasing buffer size or processing rate.")
	}
}

var locationHub = NewLocationHub()

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// authenticateUserForWebSocket extracts and validates the JWT token from the Gin context,
// determining the user's role (driver/sacco/commuter) and their associated IDs.
func authenticateUserForWebSocket(c *gin.Context) (userID uint, role string, saccoID uint, driverID uint, err error) {
	tokenString := c.Query("token")
	if tokenString == "" {
		logrus.Warn("WebSocket connection attempt: Missing token query parameter.")
		return 0, "", 0, 0, errors.New("missing authentication token")
	}

	logrus.WithField("token_snippet", tokenString[:min(len(tokenString), 30)]+"...").Debug("Received WebSocket connection request with token.")

	claims, err := middleware.ValidateToken(tokenString)
	if err != nil {
		return 0, "", 0, 0, fmt.Errorf("invalid token: %w", err)
	}

	userID = claims.UserID
	role = claims.Role

	switch role {
	case "driver":
		var driver models.Driver
		if err := config.DB.Where("user_id = ?", userID).First(&driver).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return 0, "", 0, 0, fmt.Errorf("driver profile not found for user ID %d", userID)
			}
			return 0, "", 0, 0, fmt.Errorf("database error fetching driver profile for user ID %d: %w", userID, err)
		}
		driverID = driver.ID
		saccoID = driver.SaccoID
	case "sacco":
		var sacco models.Sacco
		if err := config.DB.Where("user_id = ?", userID).First(&sacco).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return 0, "", 0, 0, fmt.Errorf("sacco profile not found for user ID %d", userID)
			}
			return 0, "", 0, 0, fmt.Errorf("database error fetching sacco profile for user ID %d: %w", userID, err)
		}
		saccoID = sacco.ID
	case "commuter":
		var user models.User
		if err := config.DB.Where("id = ? AND role = ?", userID, role).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return 0, "", 0, 0, fmt.Errorf("user with ID %d and role '%s' not found", userID, role)
			}
			return 0, "", 0, 0, fmt.Errorf("database error fetching user for ID %d: %w", userID, err)
		}
		
		saccoIDString := c.Query("sacco_id")
		if saccoIDString == "" {
			return 0, "", 0, 0, errors.New("missing 'sacco_id' query parameter for commuter connection. Commuters must specify which Sacco they want to monitor.")
		}
		parsedSaccoID, err := strconv.ParseUint(saccoIDString, 10, 64)
		if err != nil {
			return 0, "", 0, 0, fmt.Errorf("invalid 'sacco_id' parameter for commuter: %w", err)
		}
		saccoID = uint(parsedSaccoID)
		driverID = 0
	default:
		return 0, "", 0, 0, errors.New("unauthorized role for WebSocket connection")
	}
	return userID, role, saccoID, driverID, nil
}

// handleDriverWebSocket manages the WebSocket connection for a driver.
func handleDriverWebSocket(conn *websocket.Conn, driverID, saccoID uint) {
	logrus.WithFields(logrus.Fields{
		"driver_id": driverID,
		"sacco_id":  saccoID,
		"conn_ptr":  fmt.Sprintf("%p", conn),
	}).Info("Driver WebSocket connection established.")

	for {
		messageType, p, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logrus.WithField("driver_id", driverID).Info("Driver WebSocket closed normally or abnormally.")
			} else {
				logrus.WithError(err).Errorf("Error reading WebSocket message from Driver ID %d", driverID)
			}
			break
		}
		if messageType == websocket.TextMessage {
			processDriverLocation(conn, p, driverID, saccoID)
		}
	}
	logrus.WithFields(logrus.Fields{
		"driver_id": driverID,
		"conn_ptr":  fmt.Sprintf("%p", conn),
	}).Info("Driver WebSocket connection closed.")
}

// handleSaccoWebSocket manages the WebSocket connection for a Sacco client.
func handleSaccoWebSocket(conn *websocket.Conn, saccoID uint) {
	logrus.WithFields(logrus.Fields{
		"sacco_id": saccoID,
		"conn_ptr": fmt.Sprintf("%p", conn),
	}).Info("Sacco WebSocket connection established (Monitoring).")

	locationHub.RegisterClient(saccoID, conn)
	defer locationHub.UnregisterClient(saccoID, conn)

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logrus.WithField("sacco_id", saccoID).Info("Sacco monitoring WebSocket closed normally or abnormally.")
			} else {
				logrus.WithError(err).Errorf("Error reading WebSocket message from Sacco ID %d", saccoID)
			}
			break
		}
		logrus.WithField("sacco_id", saccoID).Warn("Sacco client sent unexpected message. Ignoring.")
	}
	logrus.WithFields(logrus.Fields{
		"sacco_id": saccoID,
		"conn_ptr": fmt.Sprintf("%p", conn),
	}).Info("Sacco WebSocket connection closed.")
}

// handleCommuterWebSocket manages the WebSocket connection for a Commuter client.
func handleCommuterWebSocket(conn *websocket.Conn, saccoID uint) {
	logrus.WithFields(logrus.Fields{
		"commuter_sacco_id": saccoID,
		"conn_ptr":          fmt.Sprintf("%p", conn),
	}).Info("Commuter WebSocket connection established (Monitoring).")

	locationHub.RegisterClient(saccoID, conn)
	defer locationHub.UnregisterClient(saccoID, conn)

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logrus.WithField("commuter_sacco_id", saccoID).Info("Commuter monitoring WebSocket closed normally or abnormally.")
			} else {
				logrus.WithError(err).Errorf("Error reading WebSocket message from Commuter (Sacco ID %d)", saccoID)
			}
			break
		}
		logrus.WithField("commuter_sacco_id", saccoID).Warn("Commuter client sent unexpected message. Ignoring.")
	}
	logrus.WithFields(logrus.Fields{
		"commuter_sacco_id": saccoID,
		"conn_ptr":          fmt.Sprintf("%p", conn),
	}).Info("Commuter WebSocket connection closed.")
}


// HandleLocationWebSocket is the main Gin handler for all WebSocket connections.
// It authenticates the user based on a JWT token in the query parameter and then
// delegates to the appropriate handler (driver, sacco, or commuter) based on the user's role.
// @Summary Universal WebSocket Endpoint for Drivers, Saccos, and Commuters
// @Description Establishes a WebSocket connection. Drivers send location, Saccos and Commuters receive location.
// @Produce json
// @Router /ws/location [get]
// @Tags WebSocket
// @Security BearerAuth
// @Param token query string true "JWT token for authentication"
// @Param sacco_id query integer false "Sacco ID to monitor (required for commuter role)"
func HandleLocationWebSocket(c *gin.Context) {
	userID, role, saccoID, driverID, authErr := authenticateUserForWebSocket(c)
	if authErr != nil {
		status := http.StatusUnauthorized
		if errors.Is(authErr, errors.New("unauthorized role for WebSocket connection")) {
			status = http.StatusForbidden
		}
		logrus.WithError(authErr).Warnf("WebSocket connection attempt failed for User ID %d, Role %s", userID, role)
		c.JSON(status, gin.H{"error": authErr.Error()})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logrus.WithError(err).Error("Failed to upgrade WebSocket connection.")
		return
	}
	defer conn.Close()

	if role == "driver" {
		handleDriverWebSocket(conn, driverID, saccoID)
	} else if role == "sacco" {
		handleSaccoWebSocket(conn, saccoID)
	} else if role == "commuter" {
		handleCommuterWebSocket(conn, saccoID)
	} else {
		logrus.WithFields(logrus.Fields{"user_id": userID, "role": role}).Error("Unhandled user role for WebSocket connection.")
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "Unauthorized role"))
	}
}

// processDriverLocation handles incoming location messages from a driver.
// It unmarshals the data, performs security checks, applies movement logic,
// and then calls `saveAndPublishLocation` to persist and broadcast.
func processDriverLocation(driverConn *websocket.Conn, p []byte, authenticatedDriverID uint, saccoID uint) {
	var locData LocationData // LocationData has custom UnmarshalJSON
	if err := json.Unmarshal(p, &locData); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"driver_id": authenticatedDriverID,
			"payload":   string(p),
		}).Error("Error unmarshaling location data from Driver (via custom UnmarshalJSON).") // Updated log message
		driverConn.WriteJSON(gin.H{"error": "Invalid location data format. Check timestamp format."})
		return
	}

	// Log the detailed incoming location data, now successfully unmarshaled by custom method.
	logrus.WithFields(logrus.Fields{
		"driver_id": locData.DriverID,
		"latitude":  locData.Latitude,
		"longitude": locData.Longitude,
		"accuracy":  locData.Accuracy,
		"speed":     locData.Speed,
		"bearing":   locData.Bearing,
		"altitude":  locData.Altitude,
		"timestamp": locData.Timestamp.Format(time.RFC3339Nano), // locData.Timestamp is now time.Time
	}).Info("Received driver location update via WebSocket.")

	// SECURITY CHECK: Ensure the `driver_id` in the payload matches the authenticated `driver_id`.
	if locData.DriverID != authenticatedDriverID {
		logrus.WithFields(logrus.Fields{
			"authenticated_driver_id": authenticatedDriverID,
			"payload_driver_id":       locData.DriverID,
		}).Warn("SECURITY ALERT: Driver attempted to send location for a different Driver ID. Denying.")
		driverConn.WriteJSON(gin.H{"error": "Unauthorized location update."})
		return
	}

	// Fetch the last known location for this driver from the database.
	var lastLocation models.LocationHistory
	err := config.DB.Where("driver_id = ?", locData.DriverID).Order("created_at desc").First(&lastLocation).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		saveAndPublishLocation(driverConn, locData, 0, 0, true, "initial", saccoID)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("Database error fetching last location for Driver ID %d", locData.DriverID)
		driverConn.WriteJSON(gin.H{"error": "Database error fetching last location."})
		return
	}

	currentLocationForCalc := models.LocationHistory{
		Latitude:  locData.Latitude,
		Longitude: locData.Longitude,
		Timestamp: locData.Timestamp, // locData.Timestamp is now time.Time
		Speed:     locData.Speed,
		Bearing:   locData.Bearing,
		Altitude:  locData.Altitude,
	}

	distance := calculateDistance(lastLocation.Latitude, lastLocation.Longitude, currentLocationForCalc.Latitude, currentLocationForCalc.Longitude)
	timeDiff := currentLocationForCalc.Timestamp.Sub(lastLocation.Timestamp).Seconds() // Both are time.Time now

	var currentSpeed = locData.Speed
	if currentSpeed < 0 {
		currentSpeed = 0
	}

	bearing := calculateBearing(lastLocation.Latitude, lastLocation.Longitude, currentLocationForCalc.Latitude, currentLocationForCalc.Longitude)

	isSignificant, eventType := shouldSaveLocation(distance, currentSpeed, timeDiff, lastLocation)

	if isSignificant {
		saveAndPublishLocation(driverConn, locData, distance, bearing, currentSpeed > 0.5, eventType, saccoID)
		logrus.WithFields(logrus.Fields{
			"driver_id": locData.DriverID,
			"event_type": eventType,
			"distance_m": fmt.Sprintf("%.2f", distance),
			"speed_mps":  fmt.Sprintf("%.2f", currentSpeed),
			"bearing_deg": fmt.Sprintf("%.2f", bearing),
		}).Info("Driver location saved and published (significant movement).")
	} else {
		driverConn.WriteMessage(websocket.TextMessage, []byte("Location received - no significant change"))
		logrus.WithFields(logrus.Fields{
			"driver_id": locData.DriverID,
			"distance_m": fmt.Sprintf("%.2f", distance),
			"speed_mps": fmt.Sprintf("%.2f", currentSpeed),
		}).Debug("Driver location received - minor movement, not saved.")
	}
}

// saveAndPublishLocation saves location data to the database and publishes it to the hub for Sacco clients.
func saveAndPublishLocation(driverConn *websocket.Conn, locData LocationData, distance, bearing float64, isMoving bool, eventType string, saccoID uint) {
	locationRecord := models.LocationHistory{
		DriverID:         locData.DriverID,
		Latitude:         locData.Latitude,
		Longitude:        locData.Longitude,
		Accuracy:         locData.Accuracy,
		Speed:            locData.Speed,
		Bearing:          bearing,
		Altitude:         locData.Altitude,
		IsMoving:         isMoving,
		DistanceFromLast: distance,
		Timestamp:        locData.Timestamp, // locData.Timestamp is now time.Time
		EventType:        eventType,
	}

	if err := config.DB.Create(&locationRecord).Error; err != nil {
		logrus.WithError(err).Errorf("Failed to save location for Driver ID %d", locData.DriverID)
		driverConn.WriteJSON(gin.H{"error": "Failed to save location."})
	} else {
		response := map[string]interface{}{
			"status":      "saved",
			"event_type":  eventType,
			"distance":    distance,
			"is_moving":   isMoving,
			"timestamp":   locData.Timestamp.Format(time.RFC3339Nano), // locData.Timestamp is time.Time
			"sequence_id": locationRecord.ID,
		}
		driverConn.WriteJSON(response)

		// --- BEGIN UPDATED LOGIC TO FETCH VEHICLE ID ---
		var vehicle models.Vehicle
		var vehicleID uint = 0 // Default to 0 if no vehicle is found or an error occurs

		// Attempt to find a vehicle associated with this driver ID in the `vehicles` table.
		// Assumes a vehicle can be uniquely identified by its DriverID.
		if err := config.DB.Where("driver_id = ?", locData.DriverID).First(&vehicle).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				logrus.WithField("driver_id", locData.DriverID).Warn("No vehicle found associated with this driver. Using 0 for broadcast.")
			} else {
				logrus.WithError(err).WithField("driver_id", locData.DriverID).Error("Database error fetching vehicle for driver. Using 0 for broadcast.")
			}
		} else {
			// If a vehicle is found, use its ID.
			vehicleID = vehicle.ID
			logrus.WithFields(logrus.Fields{
				"driver_id": locData.DriverID,
				"vehicle_id": vehicleID,
			}).Debug("Successfully found vehicle for driver.")
		}
		// --- END UPDATED LOGIC ---

		// Explicitly cast saccoID to float64 for broadcast map consistency.
		broadcastData := map[string]interface{}{
			"driver_id":   locData.DriverID,
			"vehicle_id":  vehicleID, // This will be the found Vehicle.ID or 0
			"latitude":    locData.Latitude,
			"longitude":   locData.Longitude,
			"accuracy":    locData.Accuracy,
			"speed":       locData.Speed,
			"bearing":     bearing,
			"altitude":    locData.Altitude,
			"timestamp":   locData.Timestamp.Format(time.RFC3339Nano),
			"event_type":  eventType,
			"is_moving":   isMoving,
			"sacco_id":    float64(saccoID),           // Explicitly cast saccoID to float64
			"sequence_id": locationRecord.ID,
		}
		locationHub.PublishLocation(broadcastData)
		logrus.WithFields(logrus.Fields{
			"driver_id": locData.DriverID,
			"sacco_id":  saccoID,
			"event_type": eventType,
			"sequence_id": locationRecord.ID,
		}).Debug("Location data published to hub for Sacco clients.")
	}
}

// shouldSaveLocation implements IoT-style logic to decide if a location update is significant enough to save.
func shouldSaveLocation(distance, speed, timeDiff float64, lastLocation models.LocationHistory) (bool, string) {
	const minDistanceForSave = 5.0
	const minTimeDiffForSave = 10.0
	const minSpeedForMoving = 0.5
	const maxSpeedForStopped = 1.0

	if lastLocation.ID == 0 {
		return true, "initial"
	}

	if distance >= minDistanceForSave {
		return true, "move"
	}

	if lastLocation.IsMoving && speed < maxSpeedForStopped && timeDiff >= minTimeDiffForSave {
		return true, "stopped"
	}

	if !lastLocation.IsMoving && speed >= minSpeedForMoving && timeDiff >= minTimeDiffForSave {
		return true, "started"
	}

	const periodicSaveInterval = 60 * time.Second
	if time.Since(lastLocation.Timestamp) >= periodicSaveInterval {
		return true, "periodic"
	}

	return false, "insignificant"
}

// calculateDistance calculates the distance between two geographical points.
func calculateDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371000 // Earth's radius in meters.
	dLat := toRadians(lat2 - lat1)
	dLon := toRadians(lon2 - lon1)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRadians(lat1))*math.Cos(toRadians(lat2))*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return R * c
}

// calculateSpeed estimates the speed in meters per second.
func calculateSpeed(prev, curr models.LocationHistory) float64 {
	timeDiff := curr.Timestamp.Sub(prev.Timestamp).Seconds()
	if timeDiff <= 0 {
		return 0.0
	}
	distance := calculateDistance(prev.Latitude, prev.Longitude, curr.Latitude, curr.Longitude)
	return distance / timeDiff
}

// calculateBearing calculates the initial bearing (direction) in degrees.
func calculateBearing(lat1, lon1, lat2, lon2 float64) float64 {
	lat1Rad := toRadians(lat1)
	lon1Rad := toRadians(lon1)
	lat2Rad := toRadians(lat2)
	lon2Rad := toRadians(lon2)

	deltaLon := lon2Rad - lon1Rad

	y := math.Sin(deltaLon) * math.Cos(lat2Rad)
	x := math.Cos(lat1Rad)*math.Sin(lat2Rad) -
		math.Sin(lat1Rad)*math.Cos(lat2Rad)*math.Cos(deltaLon)
	bearingRad := math.Atan2(y, x)
	bearingDeg := toDegrees(bearingRad)

	return math.Mod(bearingDeg+360, 360)
}

// toRadians converts an angle from degrees to radians.
func toRadians(deg float64) float64 {
	return deg * math.Pi / 180
}

// toDegrees converts an angle from radians to degrees.
func toDegrees(rad float64) float64 {
	return rad * 180 / math.Pi
}
