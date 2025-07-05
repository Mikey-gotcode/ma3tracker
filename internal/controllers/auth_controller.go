package controllers

import (
    "errors"
    "net/http"
    "strings"

    "github.com/gin-gonic/gin"
    "github.com/lib/pq"
    "golang.org/x/crypto/bcrypt"
    "gorm.io/gorm"

    "ma3_tracker/internal/config"
    "ma3_tracker/internal/middleware" // Make sure this import is correct
    "ma3_tracker/internal/models"
)

type signupInput struct {
    Name          string `json:"name" binding:"required"`
    Email         string `json:"email" binding:"required,email"`
    Password      string `json:"password" binding:"required"`
    Phone         string `json:"phone"`
    Role          string `json:"role"`
    SaccoName     string `json:"sacco_name"`
    SaccoOwner    string `json:"sacco_owner"`
    DriverPhone   string `json:"driver_phone"`
    LicenseNumber string `json:"license_number"`
    SaccoID       uint   `json:"sacco_id"`
}

type changePasswordInput struct {
    OldPassword string `json:"old_password" binding:"required"`
    NewPassword string `json:"new_password" binding:"required,min=8"` // Min 8 characters
}

type updateUserInput struct {
    Name          *string `json:"name"`          // Pointer to allow null/omission
    Email         *string `json:"email"`         // Careful with email changes, usually requires verification
    Phone         *string `json:"phone"`         // Pointer for optional update

    // Specific to Sacco role
    SaccoName     *string `json:"sacco_name"`
    SaccoOwner    *string `json:"sacco_owner"`
    SaccoPhone    *string `json:"sacco_phone"` // Assuming Sacco has a phone field
    SaccoEmail    *string `json:"sacco_email"` // Assuming Sacco has an email field

    // Specific to Driver role
    DriverPhone   *string `json:"driver_phone"`
    LicenseNumber *string `json:"license_number"`
    SaccoID       *uint   `json:"sacco_id"` // Driver can change sacco, might need validation
}

// --- EXISTING CONTROLLERS (unmodified for brevity) ---

func SignupUser(c *gin.Context) {
    var input signupInput
    if err := c.ShouldBindJSON(&input); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    role, err := validateAndNormalizeRole(input.Role)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }
    input.Role = role

    hashedPassword, err := hashPassword(input.Password)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "could not hash password"})
        return
    }

    tx := config.DB.Begin()
    if tx.Error != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "could not start transaction"})
        return
    }

    user, err := createUserRecord(tx, input, hashedPassword)
    if err != nil {
        tx.Rollback()
        if pgErr, ok := err.(*pq.Error); ok && pgErr.Code == "23505" {
            c.JSON(http.StatusConflict, gin.H{"error": "email already in use"})
            return
        }
        c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create user: " + err.Error()})
        return
    }

    err = createActorRecord(tx, &user, input)
    if err != nil {
        tx.Rollback()
        if strings.Contains(err.Error(), "driver must be assigned to a sacco_id") ||
            strings.Contains(err.Error(), "sacco with the provided sacco_id does not exist") ||
            strings.Contains(err.Error(), "required for driver role") ||
            strings.Contains(err.Error(), "required for sacco role") {
            c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        } else {
            c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create actor record: " + err.Error()})
        }
        return
    }

    if err := tx.Commit().Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "could not commit transaction: " + err.Error()})
        return
    }

    token, err := middleware.GenerateToken(user.ID, user.Role)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "could not generate token"})
        return
    }

    responseUser := prepareUserResponse(user)

    c.JSON(http.StatusCreated, gin.H{
        "token": token,
        "user":  responseUser,
    })
}

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
        Preload("Driver").
        Preload("Driver.Sacco")

    if err := query.First(&user).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found or invalid credentials"})
        } else {
            c.JSON(http.StatusInternalServerError, gin.H{"error": "database error: " + err.Error()})
        }
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

    var responseUserWithAssociations models.User
    if err := config.DB.Where("id = ?", user.ID).
        Preload("Sacco").
        Preload("Driver").
        Preload("Driver.Sacco").
        First(&responseUserWithAssociations).Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load user associations for response: " + err.Error()})
        return
    }

    finalResponseUser := prepareUserResponse(responseUserWithAssociations)

    c.JSON(http.StatusOK, gin.H{
        "token": token,
        "user":  finalResponseUser,
    })
}

func ListCommuters(c *gin.Context) {
    var commuters []models.User
    if err := config.DB.Where("role = ?", "commuter").Find(&commuters).Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Error listing commuters: " + err.Error()})
        return
    }

    c.JSON(http.StatusOK, gin.H{"data": commuters})
}

func GetMyProfile(c *gin.Context) {
    // Retrieve user_id from context, set by the AuthMiddleware
    userIDFloat, ok := c.Get("user_id")
    if !ok {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "User ID not found in context"})
        return
    }
    userID := uint(userIDFloat.(float64)) // Assert as float64 then convert to uint

    var user models.User
    // Fetch the user with all its associations
    if err := config.DB.Where("id = ?", userID).
        Preload("Sacco").
        Preload("Driver").
        Preload("Driver.Sacco"). // Preload Sacco associated with the Driver
        First(&user).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            c.JSON(http.StatusNotFound, gin.H{"error": "User not found"}) // This should ideally not happen if token is valid
        } else {
            c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error: " + err.Error()})
        }
        return
    }

    // Use the existing helper to format the user response
    responseUser := prepareUserResponse(user)

    c.JSON(http.StatusOK, gin.H{
        "message": "User profile fetched successfully",
        "user":    responseUser,
    })
}


// --- NEW CONTROLLERS (MODIFIED FOR CONTEXT KEY AND TYPE ASSERTION) ---

// ChangePassword allows an authenticated user to change their password
func ChangePassword(c *gin.Context) {
    // Correctly retrieve user_id from context as float64, then convert to uint
    userIDFloat, ok := c.Get("user_id")
    if !ok {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "User ID not found in context"})
        return
    }
    userID := uint(userIDFloat.(float64)) // Assert as float64 then convert to uint
    
    var input changePasswordInput
    if err := c.ShouldBindJSON(&input); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    var user models.User
    if err := config.DB.First(&user, userID).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
        } else {
            c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error: " + err.Error()})
        }
        return
    }

    // Verify old password
    if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(input.OldPassword)); err != nil {
        c.JSON(http.StatusUnauthorized, gin.H{"error": "Incorrect old password"})
        return
    }

    // Hash new password
    hashedNewPassword, err := hashPassword(input.NewPassword)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not hash new password"})
        return
    }

    // Update password in the database
    // FIX: Use hashedNewPassword here
    if err := config.DB.Model(&user).Update("Password", hashedNewPassword).Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not update password: " + err.Error()})
        return
    }

    c.JSON(http.StatusOK, gin.H{"message": "Password changed successfully"})
}

// UpdateUserDetails allows an authenticated user to update their profile details
func UpdateUserDetails(c *gin.Context) {
    // Correctly retrieve user_id from context as float64, then convert to uint
    userIDFloat, ok := c.Get("user_id")
    if !ok {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "User ID not found in context"})
        return
    }
    userID := uint(userIDFloat.(float64)) // Assert as float64 then convert to uint
    
    // Role is already correctly retrieved as string
    role, ok := c.Get("role")
    if !ok {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Role not found in context"})
        return
    }
    userRole := role.(string)

    var input updateUserInput
    if err := c.ShouldBindJSON(&input); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    tx := config.DB.Begin() // Start a transaction for atomicity
    if tx.Error != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not start transaction"})
        return
    }

    var user models.User
    if err := tx.First(&user, userID).Error; err != nil {
        tx.Rollback()
        if errors.Is(err, gorm.ErrRecordNotFound) {
            c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
        } else {
            c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error: " + err.Error()})
        }
        return
    }

    // Update User fields
    if input.Name != nil {
        user.Name = *input.Name
    }
    if input.Phone != nil {
        user.Phone = *input.Phone
    }
    // Handle email update carefully: it's a unique field
    if input.Email != nil && user.Email != *input.Email { // Only if email is actually changing
        var existingUser models.User
        if err := tx.Where("email = ?", *input.Email).First(&existingUser).Error; err == nil {
            tx.Rollback()
            c.JSON(http.StatusConflict, gin.H{"error": "New email already in use by another account"})
            return
        } else if !errors.Is(err, gorm.ErrRecordNotFound) {
            tx.Rollback()
            c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error checking email: " + err.Error()})
            return
        }
        user.Email = *input.Email
    }

    if err := tx.Save(&user).Error; err != nil {
        tx.Rollback()
        if pgErr, ok := err.(*pq.Error); ok && pgErr.Code == "23505" { // Unique constraint violation
            c.JSON(http.StatusConflict, gin.H{"error": "Email already in use"})
            return
        }
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not update user details: " + err.Error()})
        return
    }

    // Update Actor-specific fields based on role
    switch userRole { // Use userRole obtained from context
    case "sacco":
        var sacco models.Sacco
        if err := tx.Where("user_id = ?", userID).First(&sacco).Error; err != nil {
            tx.Rollback()
            if errors.Is(err, gorm.ErrRecordNotFound) {
                c.JSON(http.StatusNotFound, gin.H{"error": "Sacco record not found for user"})
            } else {
                c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error fetching sacco: " + err.Error()})
            }
            return
        }
        if input.SaccoName != nil {
            sacco.Name = *input.SaccoName
        }
        if input.SaccoOwner != nil {
            sacco.Owner = *input.SaccoOwner
        }
        if input.SaccoPhone != nil { // Assuming Sacco model has Phone field
            sacco.Phone = *input.SaccoPhone
        }
        if input.SaccoEmail != nil { // Assuming Sacco model has Email field
            sacco.Email = *input.SaccoEmail
        }

        if err := tx.Save(&sacco).Error; err != nil {
            tx.Rollback()
            c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not update sacco details: " + err.Error()})
            return
        }

    case "driver":
        var driver models.Driver
        if err := tx.Where("user_id = ?", userID).First(&driver).Error; err != nil {
            tx.Rollback()
            if errors.Is(err, gorm.ErrRecordNotFound) {
                c.JSON(http.StatusNotFound, gin.H{"error": "Driver record not found for user"})
            } else {
                c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error fetching driver: " + err.Error()})
            }
            return
        }
        if input.DriverPhone != nil {
            driver.Phone = *input.DriverPhone
        }
        if input.LicenseNumber != nil {
            driver.LicenseNumber = *input.LicenseNumber
        }
        if input.SaccoID != nil {
            // Validate new SaccoID
            var existingSacco models.Sacco
            if result := tx.First(&existingSacco, *input.SaccoID); result.Error != nil {
                tx.Rollback()
                if errors.Is(result.Error, gorm.ErrRecordNotFound) {
                    c.JSON(http.StatusBadRequest, gin.H{"error": "Sacco with the provided sacco_id does not exist"})
                } else {
                    c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error validating sacco ID: " + result.Error.Error()})
                }
                return
            }
            driver.SaccoID = *input.SaccoID
        }

        if err := tx.Save(&driver).Error; err != nil {
            tx.Rollback()
            c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not update driver details: " + err.Error()})
            return
        }
    case "commuter":
        // No additional actor-specific details to update for commuter
    }

    if err := tx.Commit().Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not commit transaction: " + err.Error()})
        return
    }

    // Fetch the updated user with associations for the response
    var updatedUser models.User
    if err := config.DB.Where("id = ?", userID).
        Preload("Sacco").
        Preload("Driver").
        Preload("Driver.Sacco").
        First(&updatedUser).Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load updated user associations for response: " + err.Error()})
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "message": "User details updated successfully",
        "user":    prepareUserResponse(updatedUser),
    })
}

// --- EXISTING HELPER FUNCTIONS (unmodified for brevity) ---

func validateAndNormalizeRole(roleInput string) (string, error) {
    role := strings.ToLower(strings.TrimSpace(roleInput))
    if role == "" {
        role = "commuter"
    }
    switch role {
    case "commuter", "admin", "sacco", "driver":
        return role, nil
    default:
        return "", errors.New("invalid role")
    }
}

func hashPassword(password string) (string, error) {
    hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
    if err != nil {
        return "", err
    }
    return string(hash), nil
}

func createUserRecord(tx *gorm.DB, input signupInput, hashedPassword string) (models.User, error) {
    user := models.User{
        Name:     input.Name,
        Email:    input.Email,
        Password: hashedPassword,
        Phone:    input.Phone,
        Role:     input.Role,
    }
    if err := tx.Create(&user).Error; err != nil {
        return models.User{}, err
    }
    return user, nil
}

func createActorRecord(tx *gorm.DB, user *models.User, input signupInput) error {
    switch user.Role {
    case "sacco":
        if input.SaccoName == "" || input.SaccoOwner == "" {
            return errors.New("sacco_name and sacco_owner are required for sacco role")
        }

        sacco := models.Sacco{
            UserID:    user.ID,
            Name:      input.SaccoName,
            Owner:     input.SaccoOwner,
            Email:     input.Email,
            Phone:     input.Phone,
        }
        if err := tx.Create(&sacco).Error; err != nil {
            return err
        }
        user.Sacco = &sacco
        if err := tx.Save(user).Error; err != nil {
            return err
        }
    case "driver":
        if input.LicenseNumber == "" {
            return errors.New("license_number is required for driver role")
        }
        if input.SaccoID == 0 {
            return errors.New("driver must be assigned to a sacco_id")
        }

        var existingSacco models.Sacco
        if result := tx.First(&existingSacco, input.SaccoID); result.Error != nil {
            if errors.Is(result.Error, gorm.ErrRecordNotFound) {
                return errors.New("sacco with the provided sacco_id does not exist")
            }
            return result.Error
        }

        driver := models.Driver{
            UserID:        user.ID,
            Name:          input.Name,
            Phone:         input.DriverPhone,
            LicenseNumber: input.LicenseNumber,
            SaccoID:       input.SaccoID,
        }
        if err := tx.Create(&driver).Error; err != nil {
            return err
        }
        user.Driver = &driver
        if err := tx.Save(user).Error; err != nil {
            return err
        }
    }
    return nil
}

func prepareUserResponse(user models.User) gin.H {
    responseUser := gin.H{
        "ID":        user.ID,
        "CreatedAt": user.CreatedAt,
        "UpdatedAt": user.UpdatedAt,
        "name":      user.Name,
        "email":     user.Email,
        "phone":     user.Phone,
        "role":      user.Role,
    }

    if user.Sacco != nil {
        responseUser["sacco"] = gin.H{
            "ID":        user.Sacco.ID,
            "CreatedAt": user.Sacco.CreatedAt,
            "UpdatedAt": user.Sacco.UpdatedAt,
            "name":      user.Sacco.Name,
            "owner":     user.Sacco.Owner,
            "email":     user.Sacco.Email,
            "phone":     user.Sacco.Phone,
        }
        responseUser["sacco_id"] = user.Sacco.ID
    }
    if user.Driver != nil {
        driverMap := gin.H{
            "ID":             user.Driver.ID,
            "CreatedAt":      user.Driver.CreatedAt,
            "UpdatedAt":      user.Driver.UpdatedAt,
            "name":           user.Driver.Name,
            "phone":          user.Driver.Phone,
            "license_number": user.Driver.LicenseNumber,
            "sacco_id":       user.Driver.SaccoID,
        }
        // Ensure Driver.Sacco is preloaded correctly before accessing
        // FIX: Change `user.Driver.Sacco != nil` to `user.Driver.Sacco.ID != 0`
        if user.Driver.Sacco.ID != 0 { // This is the correct way to check if an associated struct is "zero-value"
            driverMap["sacco"] = gin.H{
                "ID":        user.Driver.Sacco.ID,
                "name":      user.Driver.Sacco.Name,
                "owner":     user.Driver.Sacco.Owner,
            }
        }
        responseUser["driver"] = driverMap
        if user.Driver.SaccoID != 0 {
            responseUser["sacco_id"] = user.Driver.SaccoID
        }
    }
    return responseUser
}
