package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/Shivanand-hulikatti/ai-grader/internal/auth"
	"github.com/Shivanand-hulikatti/ai-grader/internal/models"
)

// ========== HEALTH CHECK ==========
func healthHandler(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{
		"status":  "healthy",
		"service": "api-gateway",
		"time":    time.Now().Format(time.RFC3339),
	})
}

// ========== REGISTER ==========
func registerHandler(w http.ResponseWriter, r *http.Request) {
	var req models.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
		return
	}

	// Validate input
	if req.Email == "" || req.Password == "" || req.FullName == "" {
		respondError(w, http.StatusBadRequest, "missing_fields", "email, password, and full_name required")
		return
	}

	if len(req.Password) < 8 {
		respondError(w, http.StatusBadRequest, "weak_password", "Password must be at least 8 characters")
		return
	}

	// Check if email already exists
	existing, err := authRepo.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "database_error", "Failed to check email")
		return
	}
	if existing != nil {
		respondError(w, http.StatusConflict, "email_exists", "Email already registered")
		return
	}

	// Hash password
	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "hash_error", "Failed to hash password")
		return
	}

	// Create user
	user := &models.User{
		Email:        req.Email,
		PasswordHash: passwordHash,
		FullName:     req.FullName,
	}

	if err := authRepo.CreateUser(r.Context(), user); err != nil {
		log.Printf("Failed to create user: %v", err)
		respondError(w, http.StatusInternalServerError, "create_error", "Failed to create user")
		return
	}

	// Generate tokens
	tokens, err := auth.GenerateTokenPair(user.ID, user.Email, jwtSecret)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "token_error", "Failed to generate tokens")
		return
	}

	// Store refresh token
	tokenHash := auth.HashRefreshToken(tokens.RefreshToken)
	refreshToken := &models.RefreshToken{
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour), // 30 days
	}

	if err := authRepo.SaveRefreshToken(r.Context(), refreshToken); err != nil {
		log.Printf("Failed to save refresh token: %v", err)
		// Non-critical error, continue
	}

	// Return response
	user.PasswordHash = "" // Don't send password hash
	respondJSON(w, http.StatusCreated, models.AuthResponse{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresIn:    tokens.ExpiresIn,
		User:         *user,
	})
}

// ========== LOGIN ==========
func loginHandler(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
		return
	}

	// Validate input
	if req.Email == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, "missing_fields", "email and password required")
		return
	}

	// Get user by email
	user, err := authRepo.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "database_error", "Failed to fetch user")
		return
	}
	if user == nil {
		respondError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password")
		return
	}

	// Verify password
	if !auth.CheckPassword(req.Password, user.PasswordHash) {
		respondError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password")
		return
	}

	// Generate tokens
	tokens, err := auth.GenerateTokenPair(user.ID, user.Email, jwtSecret)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "token_error", "Failed to generate tokens")
		return
	}

	// Store refresh token
	tokenHash := auth.HashRefreshToken(tokens.RefreshToken)
	refreshToken := &models.RefreshToken{
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
	}

	if err := authRepo.SaveRefreshToken(r.Context(), refreshToken); err != nil {
		log.Printf("Failed to save refresh token: %v", err)
	}

	// Return response
	user.PasswordHash = ""
	respondJSON(w, http.StatusOK, models.AuthResponse{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresIn:    tokens.ExpiresIn,
		User:         *user,
	})
}

// ========== REFRESH TOKEN ==========
func refreshTokenHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
		return
	}

	if req.RefreshToken == "" {
		respondError(w, http.StatusBadRequest, "missing_token", "refresh_token required")
		return
	}

	// Hash and look up token
	tokenHash := auth.HashRefreshToken(req.RefreshToken)
	token, err := authRepo.GetRefreshToken(r.Context(), tokenHash)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "database_error", "Failed to verify token")
		return
	}
	if token == nil {
		respondError(w, http.StatusUnauthorized, "invalid_token", "Invalid or expired refresh token")
		return
	}

	// Get user
	user, err := authRepo.GetUserByID(r.Context(), token.UserID)
	if err != nil || user == nil {
		respondError(w, http.StatusUnauthorized, "user_not_found", "User not found")
		return
	}

	// Generate new token pair
	tokens, err := auth.GenerateTokenPair(user.ID, user.Email, jwtSecret)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "token_error", "Failed to generate tokens")
		return
	}

	// Store new refresh token
	newTokenHash := auth.HashRefreshToken(tokens.RefreshToken)
	newRefreshToken := &models.RefreshToken{
		UserID:    user.ID,
		TokenHash: newTokenHash,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
	}

	if err := authRepo.SaveRefreshToken(r.Context(), newRefreshToken); err != nil {
		log.Printf("Failed to save new refresh token: %v", err)
	}

	// Revoke old refresh token
	authRepo.RevokeRefreshToken(r.Context(), tokenHash)

	// Return response
	user.PasswordHash = ""
	respondJSON(w, http.StatusOK, models.AuthResponse{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresIn:    tokens.ExpiresIn,
		User:         *user,
	})
}

// ========== HELPERS ==========
func respondJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, code int, errorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   errorCode,
		"message": message,
	})
}
