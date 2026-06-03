package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"ds-chat2api/server/internal/transport"
	"ds-chat2api/server/internal/web"
)

func main() {
	wsPort := "8765"
	httpPort := "8766"
	if len(os.Args) > 2 {
		wsPort = os.Args[1]
		httpPort = os.Args[2]
	} else if len(os.Args) > 1 {
		wsPort = os.Args[1]
	}

	ws := transport.NewWsServer()
	ws.Start(wsPort)

	apiKey := os.Getenv("API_KEY")
	hs := web.NewHttpServer(ws, ws, apiKey)
	hs.Start(httpPort)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("Shutting down...")
}
