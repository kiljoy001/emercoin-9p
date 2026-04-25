package main

import (
	"flag"
	"log"

	"emercoin9p"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "address to bind the 9p server to; use :0 for a random port")
	flag.Parse()

	ns, err := emercoin9p.NewNsFromEnv()
	if err != nil {
		log.Fatalf("failed to initialize namespace: %v", err)
	}

	server, err := emercoin9p.Listen(ns, *addr)
	if err != nil {
		log.Fatalf("failed to start 9p server: %v", err)
	}
	defer server.Close()

	log.Printf("emercoin9p listening on %s", server.Addr())
	log.Printf("root ctl is available at /ctl; write `port` to retrieve the bound port")

	if err := server.Wait(); err != nil {
		log.Fatalf("9p server stopped: %v", err)
	}
}
