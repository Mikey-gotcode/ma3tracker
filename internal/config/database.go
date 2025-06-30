package config

import (
	"fmt"
	"log"
	"os"
	"github.com/joho/godotenv"  
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"ma3_tracker/internal/models"
)

var (
	// DB is the globally accessible database handle
	DB *gorm.DB
)

// InitDB initializes the database connection using environment variables
// and applies PostGIS and TimescaleDB extensions.
func InitDB() {
	 // 1) Load .env (if present)
    if err := godotenv.Load(); err != nil {
        log.Println("No .env file found – relying on env vars")
    }

	// Load environment variables (with defaults)
	host := getEnv("DB_HOST", "localhost")
	port := getEnv("DB_PORT", "5432")
	user := getEnv("DB_USER", "postgres")
	password := getEnv("DB_PASSWORD", "password")
	dbname := getEnv("DB_NAME", "tracker")
	sslmode := getEnv("DB_SSLMODE", "disable")
	timezone := getEnv("DB_TIMEZONE", "UTC")

	// Build Data Source Name
	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=%s TimeZone=%s",
		host, user, password, dbname, port, sslmode, timezone,
	)

	// Open GORM connection
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	// Enable necessary extensions
	db.Exec("CREATE EXTENSION IF NOT EXISTS postgis;")
	db.Exec("CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE;")

	// Auto-migrate your user model (optional but recommended)
	err = db.AutoMigrate(&models.User{},&models.Driver{},&models.Sacco{},&models.Route{},&models.Vehicle{},&models.Stage{}, &models.LocationHistory{})
	if err != nil {
		log.Fatalf("auto-migration failed: %v", err)
	}


	// Assign to global
	DB = db
}

// getEnv reads an environment variable or returns the provided default
func getEnv(key, defaultValue string) string {
	if v, exists := os.LookupEnv(key); exists {
		return v
	}
	return defaultValue
}

// GetDB returns the initialized DB handle
func GetDB() *gorm.DB {
	return DB
}
