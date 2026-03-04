package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	internalwebrtc "stream-sniff/internal/webrtc"

	"github.com/joho/godotenv"
)

const (
	envFileProd = ".env.production"
	envFileDev  = ".env.development"
)

var (
	errAuthorizationNotSet = errors.New("authorization was not set")
	errInvalidBearerToken  = errors.New("invalid Bearer Token format")

	bearerTokenRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-\.~]+$`)
)

func getBearerToken(r *http.Request) (string, error) {
	authorizationHeader := r.Header.Get("Authorization")
	if authorizationHeader == "" {
		return "", errAuthorizationNotSet
	}

	const bearerPrefix = "Bearer "
	if !strings.HasPrefix(authorizationHeader, bearerPrefix) {
		return "", errInvalidBearerToken
	}

	bearerToken := strings.TrimPrefix(authorizationHeader, bearerPrefix)
	if !bearerTokenRegex.MatchString(bearerToken) {
		return "", errInvalidBearerToken
	}

	return bearerToken, nil
}

func logHTTPError(w http.ResponseWriter, err string, code int) {
	log.Println(err)
	http.Error(w, err, code)
}

func offerHandler(location string, negotiate func(offer, bearerToken string) (string, error)) http.HandlerFunc {
	return func(res http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}

		bearerToken, err := getBearerToken(r)
		if err != nil {
			logHTTPError(res, err.Error(), http.StatusBadRequest)
			return
		}

		offer, err := io.ReadAll(r.Body)
		if err != nil {
			logHTTPError(res, err.Error(), http.StatusBadRequest)
			return
		}

		answer, err := negotiate(string(offer), bearerToken)
		if err != nil {
			logHTTPError(res, err.Error(), http.StatusBadRequest)
			return
		}

		res.Header().Add("Location", location)
		res.Header().Add("Content-Type", "application/sdp")
		res.WriteHeader(http.StatusCreated)
		if _, err = fmt.Fprint(res, answer); err != nil {
			log.Println(err)
		}
	}
}

func indexHandler(res http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(res, r)
		return
	}

	http.ServeFile(res, r, "index.html")
}

func corsHandler(next func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
	return func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Access-Control-Allow-Origin", "*")
		res.Header().Set("Access-Control-Allow-Methods", "*")
		res.Header().Set("Access-Control-Allow-Headers", "*")
		res.Header().Set("Access-Control-Expose-Headers", "*")

		if req.Method != http.MethodOptions {
			next(res, req)
		}
	}
}

func loadConfigs() error {
	if os.Getenv("APP_ENV") == "development" {
		log.Println("Loading `" + envFileDev + "`")
		return godotenv.Load(envFileDev)
	}

	log.Println("Loading `" + envFileProd + "`")
	return godotenv.Load(envFileProd)
}

func main() {
	if err := loadConfigs(); err != nil {
		log.Println("Failed to find config in CWD, changing CWD to executable path")

		exePath, execErr := os.Executable()
		if execErr != nil {
			log.Fatal(execErr)
		}

		if err = os.Chdir(filepath.Dir(exePath)); err != nil {
			log.Fatal(err)
		}

		if err = loadConfigs(); err != nil {
			log.Fatal(err)
		}
	}

	internalwebrtc.Configure()

	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/api/whip", corsHandler(offerHandler("/api/whip", internalwebrtc.WHIP)))
	http.HandleFunc("/api/analyze", corsHandler(offerHandler("/api/analyze", internalwebrtc.Analyze)))

	server := &http.Server{
		Addr: os.Getenv("HTTP_ADDRESS"),
	}

	log.Println("Running HTTP Server at `" + os.Getenv("HTTP_ADDRESS") + "`")
	log.Fatal(server.ListenAndServe())
}
