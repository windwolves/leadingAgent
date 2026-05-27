package main

import (
	"log"

	"github.com/example/deepseek-go/config"
	"github.com/example/deepseek-go/handlers"
	"github.com/apache/thrift/lib/go/thrift"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	handler := handlers.NewLdAgentHandler(cfg)
	processor := handler.Processor()

	transport, err := thrift.NewTServerSocket(":9090")
	if err != nil {
		log.Fatalf("Failed to create socket: %v", err)
	}

	transportFactory := thrift.NewTBufferedTransportFactory(8192)
	protocolFactory := thrift.NewTBinaryProtocolFactoryDefault()

	server := thrift.NewTSimpleServer4(processor, transport, transportFactory, protocolFactory)

	log.Println("Starting Thrift server on port 9090...")
	if err := server.Serve(); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
