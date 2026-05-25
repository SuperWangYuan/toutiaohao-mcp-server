package main

import (
	"context"
	"flag"

	"github.com/example/toutiaohao-mcp-server/cookies"
	log "github.com/sirupsen/logrus"
)

func main() {
	port := flag.String("port", "8080", "HTTP server port")
	loginMode := flag.Bool("login", false, "Start interactive QR code login to save cookies and then exit")
	flag.Parse()

	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})

	cookieStore := cookies.NewFileCookieStore(cookies.GetDefaultCookiePath())
	service := NewToutiaoService(cookieStore)

	if *loginMode {
		log.Info("Starting Toutiao MCP Server in Login Mode...")
		ctx := context.Background()
		if err := service.QrCodeLogin(ctx); err != nil {
			log.Fatalf("QR code login failed: %v", err)
		}
		log.Info("Login complete, cookies saved to cookies.json. Exiting...")
		return
	}

	log.Info("Starting Toutiao MCP Server...")
	server := NewAppServer(service)

	if err := server.Start(*port); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
