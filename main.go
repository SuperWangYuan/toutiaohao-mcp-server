package main

import (
	"flag"

	"github.com/example/toutiaohao-mcp-server/cookies"
	log "github.com/sirupsen/logrus"
)

func main() {
	port := flag.String("port", "8080", "HTTP server port")
	flag.Parse()

	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
	log.Info("Starting Toutiao MCP Server...")

	cookieStore := cookies.NewFileCookieStore(cookies.GetDefaultCookiePath())
	service := NewToutiaoService(cookieStore)
	server := NewAppServer(service)

	if err := server.Start(*port); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
