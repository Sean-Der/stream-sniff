package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	defaultAddr := ":8080"
	if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
		defaultAddr = ":" + port
	}

	listenAddr := flag.String("listen", defaultAddr, "HTTP listen address")
	pagePath := flag.String("page", "index.html", "path to the HTML file")
	flag.Parse()

	http.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, *pagePath)
	})

	httpServer := &http.Server{
		Addr:              *listenAddr,
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger := log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds)
	logger.Printf("serving %s on %s", *pagePath, *listenAddr)
	if err := httpServer.ListenAndServe(); err != nil {
		logger.Fatalf("server stopped: %v", err)
	}
}
