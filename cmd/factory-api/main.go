package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"dark-factory/internal/factoryapi"
)

func main() {
	addr := strings.TrimSpace(os.Getenv("FACTORY_API_ADDR"))
	if addr == "" {
		addr = ":8080"
	}
	srv := factoryapi.NewServer(nil)
	log.Printf("factory-api listening on %s", addr)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatalf("factory-api failed: %v", err)
	}
}
