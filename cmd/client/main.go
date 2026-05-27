package main

import (
	"context"
	"fmt"

	"github.com/example/deepseek-go/services"
	"github.com/apache/thrift/lib/go/thrift"
)

func main() {
	transport, err := thrift.NewTSocket("localhost:9090")
	if err != nil {
		fmt.Printf("Failed to create socket: %v\n", err)
		return
	}

	protocolFactory := thrift.NewTBinaryProtocolFactoryDefault()
	iprot := protocolFactory.GetProtocol(transport)
	oprot := protocolFactory.GetProtocol(transport)

	if err := transport.Open(); err != nil {
		fmt.Printf("Failed to open transport: %v\n", err)
		return
	}
	defer transport.Close()

	request := &services.ChatRequest{
		Message:   "Hello, how are you?",
		SessionId: "test-session-001",
	}

	oprot.WriteMessageBegin(context.Background(), "chat", thrift.CALL, 0)
	
	if err := oprot.WriteStructBegin(context.Background(), "chat_args"); err != nil {
		fmt.Printf("Failed to write struct begin: %v\n", err)
		return
	}

	if err := oprot.WriteFieldBegin(context.Background(), "message", thrift.STRING, 1); err != nil {
		fmt.Printf("Failed to write field begin: %v\n", err)
		return
	}
	if err := oprot.WriteString(context.Background(), request.Message); err != nil {
		fmt.Printf("Failed to write message: %v\n", err)
		return
	}
	if err := oprot.WriteFieldEnd(context.Background()); err != nil {
		fmt.Printf("Failed to write field end: %v\n", err)
		return
	}

	if err := oprot.WriteFieldStop(context.Background()); err != nil {
		fmt.Printf("Failed to write field stop: %v\n", err)
		return
	}

	if err := oprot.WriteStructEnd(context.Background()); err != nil {
		fmt.Printf("Failed to write struct end: %v\n", err)
		return
	}

	if err := oprot.WriteMessageEnd(context.Background()); err != nil {
		fmt.Printf("Failed to write message end: %v\n", err)
		return
	}

	if err := oprot.Flush(context.Background()); err != nil {
		fmt.Printf("Failed to flush: %v\n", err)
		return
	}

	_, _, _, err = iprot.ReadMessageBegin(context.Background())
	if err != nil {
		fmt.Printf("Failed to read message begin: %v\n", err)
		return
	}

	var response services.ChatResponse
	_, err = iprot.ReadStructBegin(context.Background())
	if err != nil {
		fmt.Printf("Failed to read struct begin: %v\n", err)
		return
	}

	for {
		_, fieldTypeId, fieldId, err := iprot.ReadFieldBegin(context.Background())
		if err != nil {
			fmt.Printf("Failed to read field begin: %v\n", err)
			return
		}
		if fieldTypeId == thrift.STOP {
			break
		}
		switch fieldId {
		case 1:
			if fieldTypeId == thrift.STRING {
				val, _ := iprot.ReadString(context.Background())
				response.Response = val
			} else {
				iprot.Skip(context.Background(), fieldTypeId)
			}
		case 2:
			if fieldTypeId == thrift.STRING {
				val, _ := iprot.ReadString(context.Background())
				response.SessionId = val
			} else {
				iprot.Skip(context.Background(), fieldTypeId)
			}
		case 3:
			if fieldTypeId == thrift.BOOL {
				val, _ := iprot.ReadBool(context.Background())
				response.Success = val
			} else {
				iprot.Skip(context.Background(), fieldTypeId)
			}
		case 4:
			if fieldTypeId == thrift.STRING {
				val, _ := iprot.ReadString(context.Background())
				response.Error = val
			} else {
				iprot.Skip(context.Background(), fieldTypeId)
			}
		default:
			iprot.Skip(context.Background(), fieldTypeId)
		}
		iprot.ReadFieldEnd(context.Background())
	}

	iprot.ReadStructEnd(context.Background())
	iprot.ReadMessageEnd(context.Background())

	fmt.Println("=== Response ===")
	fmt.Printf("Success: %v\n", response.Success)
	fmt.Printf("Response: %s\n", response.Response)
	fmt.Printf("SessionId: %s\n", response.SessionId)
	if response.Error != "" {
		fmt.Printf("Error: %s\n", response.Error)
	}
}
