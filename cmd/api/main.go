package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"github.com/Shivanand-hulikatti/ai-grader/internal/auth"
	"github.com/Shivanand-hulikatti/ai-grader/internal/database"

	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
)

var (
	jwtSecret string
	authRepo  *auth.Repository
)

// proxyTransport is shared by all reverse proxies.
// DisableKeepAlives is intentionally left false (we want connection pooling)
// but IdleConnTimeout is short so stale connections are not reused for POSTs,
// which is the most common cause of "unexpected EOF" on a reverse proxy.
var proxyTransport http.RoundTripper = &http.Transport{
	DisableKeepAlives:   false,
	MaxIdleConnsPerHost: 10,
	IdleConnTimeout:     30 * time.Second,
	// Must be >= the upstream handler's own timeout (upload handler: 4 min).
	ResponseHeaderTimeout: 5 * time.Minute,
	ExpectContinueTimeout: 1 * time.Second,
}

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
	uploadServiceURL := os.Getenv("UPLOAD_SERVICE_URL")
	if uploadServiceURL == "" {
		uploadServiceURL = "http://localhost:" + os.Getenv("UPLOAD_SERVICE_PORT")
	}
	uploadURL, _ := url.Parse(uploadServiceURL)
	uploadProxy := newProxy(uploadURL, "upload-service")
	protected.PathPrefix("/upload").Handler(uploadProxy)

	// Results Service proxy
	resultsServiceURL := os.Getenv("RESULTS_SERVICE_URL")
	if resultsServiceURL == "" {
		resultsServiceURL = "http://localhost:" + os.Getenv("RESULTS_SERVICE_PORT")
	}
	resultsURL, _ := url.Parse(resultsServiceURL)
	resultsProxy := newProxy(resultsURL, "results-service")
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

// newProxy builds a reverse proxy for the given backend URL.
// It injects the authenticated user's ID as X-User-ID and provides a
// structured JSON error handler so clients always get valid JSON back.
func newProxy(target *url.URL, serviceName string) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = proxyTransport

	// FlushInterval -1 means "flush immediately" — required for streaming
	// request bodies (multipart uploads) so bytes reach the backend as they
	// arrive instead of being buffered until the upload completes.
	proxy.FlushInterval = -1

	// Wrap the default Director to inject the X-User-ID header.
	defaultDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		defaultDirector(req)
		// The auth middleware stored claims in the request context; pull them
		// out and forward the user ID to the downstream service.
		if claims, ok := auth.GetUserFromContext(req); ok {
			req.Header.Set("X-User-ID", claims.UserID)
		}
	}

	// ErrorHandler returns structured JSON instead of Go's default plain-text
	// "Bad Gateway" page, and logs enough detail to diagnose connection errors.
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("[proxy] %s error for %s %s: %v", serviceName, r.Method, r.URL.Path, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "upstream_unavailable",
			"message": serviceName + " is temporarily unavailable, please retry",
		})
	}

	return proxy
}
