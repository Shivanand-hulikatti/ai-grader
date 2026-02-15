package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"github.com/Shivanand-hulikatti/ai-grader/internal/auth"
	"github.com/Shivanand-hulikatti/ai-grader/internal/database"

	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
)

var (
	jwtSecret string
	authRepo  *auth.Repository
)

func main() {
	// Load environment
	godotenv.Load()

	// Connect to database
	if err := database.Connect(); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	// Initialize JWT secret
	jwtSecret = os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("JWT_SECRET not set in environment")
	}

	// Initialize auth repository
	authRepo = auth.NewRepository(database.Pool)

	// Create router
	router := mux.NewRouter()

	// ========== PUBLIC ROUTES (No Authentication) ==========
	router.HandleFunc("/health", healthHandler).Methods("GET")
	router.HandleFunc("/auth/register", registerHandler).Methods("POST")
	router.HandleFunc("/auth/login", loginHandler).Methods("POST")
	router.HandleFunc("/auth/refresh", refreshTokenHandler).Methods("POST")

	// ========== PROTECTED ROUTES (Authentication Required) ==========
	protected := router.PathPrefix("").Subrouter()
	protected.Use(auth.AuthMiddleware(jwtSecret))

	// Upload Service proxy
	uploadURL, _ := url.Parse("http://localhost:" + os.Getenv("UPLOAD_SERVICE_PORT"))
	uploadProxy := httputil.NewSingleHostReverseProxy(uploadURL)
	protected.PathPrefix("/upload").Handler(uploadProxy)

	// Results Service proxy
	resultsURL, _ := url.Parse("http://localhost:" + os.Getenv("RESULTS_SERVICE_PORT"))
	resultsProxy := httputil.NewSingleHostReverseProxy(resultsURL)
	protected.PathPrefix("/results").Handler(resultsProxy)

	// Start server
	port := os.Getenv("GATEWAY_PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("API Gateway starting on :%s", port)

	if err := http.ListenAndServe(":"+port, router); err != nil {
		log.Fatal(err)
	}
}
