package controllers

import (
    "net/http"
    "strings"

    "github.com/gin-gonic/gin"
    "golang.org/x/crypto/bcrypt"
	  "github.com/lib/pq"

    "ma3_tracker/internal/config"
    "ma3_tracker/internal/models"
    "ma3_tracker/internal/middleware"
)

// SignupUser creates a User, then a Sacco or Driver record if needed.
func SignupUser(c *gin.Context) {
    // Input DTO—only the common fields plus actor‐specific extras:
    var input struct {
        Name        string `json:"name" binding:"required"`
        Email       string `json:"email" binding:"required,email"`
        Password    string `json:"password" binding:"required"`
        Phone       string `json:"phone"`
        Role        string `json:"role"`
        SaccoName   string `json:"sacco_name"`
        SaccoOwner  string `json:"sacco_owner"`
        DriverPhone string `json:"driver_phone"`
    }
    if err := c.ShouldBindJSON(&input); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    // Normalize role
    role := strings.ToLower(strings.TrimSpace(input.Role))
    if role == "" {
        role = "commuter"
    }
    switch role {
    case "commuter", "admin", "sacco", "driver":
    default:
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role"})
        return
    }

    // Hash password
    hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "could not hash password"})
        return
    }

    // 1) Create user
    user := models.User{
        Name:     input.Name,
        Email:    input.Email,
        Password: string(hash),
        Phone:    input.Phone,
        Role:     role,
    }
    if err := config.DB.Create(&user).Error; err != nil {
		  if pgErr, ok := err.(*pq.Error); ok && pgErr.Code == "23505" {
            // unique_violation on email
            c.JSON(http.StatusConflict, gin.H{"error": "email already in use"})
            return
        }
        c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create user: " + err.Error()})
        return
    }

    // 2) Create actor record if sacco or driver
    switch role {
    case "sacco":
        sacco := models.Sacco{
            UserID: user.ID,
            Name:   input.SaccoName,
            Owner:  input.SaccoOwner,
            Email:  input.Email,   // you can copy fields or accept extras
            Phone:  input.Phone,
        }
        if err := config.DB.Create(&sacco).Error; err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create sacco"})
            return
        }
        user.Sacco = &sacco

    case "driver":
        driver := models.Driver{
            UserID: user.ID,
            Name:   input.Name,
            Phone:  input.DriverPhone,
        }
        if err := config.DB.Create(&driver).Error; err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create driver"})
            return
        }
        user.Driver = &driver
    }

    // 3) Issue JWT
    token, err := middleware.GenerateToken(user.ID, user.Role)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "could not generate token"})
        return
    }

    // 4) Return full profile
    c.JSON(http.StatusCreated, gin.H{
        "token": token,
        "user":  user,
    })
}

// LoginUser fetches the User and preloads Sacco/Driver if applicable.
func LoginUser(c *gin.Context) {
    var body struct {
        Email    string `json:"email" binding:"required,email"`
        Password string `json:"password" binding:"required"`
    }
    if err := c.ShouldBindJSON(&body); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    var user models.User
    query := config.DB.Where("email = ?", body.Email).
        Preload("Sacco").
        Preload("Driver")
    if err := query.First(&user).Error; err != nil {
        c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
        return
    }

    if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(body.Password)); err != nil {
        c.JSON(http.StatusUnauthorized, gin.H{"error": "incorrect password"})
        return
    }

    token, err := middleware.GenerateToken(user.ID, user.Role)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "could not generate token"})
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "token": token,
        "user":  user,
    })
}


// This method is typically for administrative use.
func ListCommuters(c *gin.Context) {
	var commuters []models.User
	// Fetch all users where the role is 'commuter'
	if err := config.DB.Where("role = ?", "commuter").Find(&commuters).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error listing commuters: " + err.Error()})
		return
	}

	// For security, you might want to omit the password hash from the response
	// by either creating a DTO (Data Transfer Object) or manually selecting fields.
	// For simplicity, we are returning the full User model for now, but be mindful of sensitive data.
	c.JSON(http.StatusOK, gin.H{"data": commuters}) // Return in a 'data' key for consistency with frontend service
}