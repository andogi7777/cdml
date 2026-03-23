package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"cdml/api"
	"cdml/node"
)

func main() {
	cfgPath := flag.String("config", "config.json", "config file path")
	flag.Parse()

	cfg, err := node.LoadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	n, err := node.New(cfg)
	if err != nil {
		log.Fatalf("create node: %v", err)
	}
	if err := n.Start(); err != nil {
		log.Fatalf("start node: %v", err)
	}

	srv := api.NewServer(cfg.APIAddr, n)
	go func() {
		if err := srv.Start(); err != nil {
			log.Printf("api server stopped: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("[cdml] shutting down...")
	srv.Stop()
	n.Stop()
}
