package services

import (
	"context"

	"github.com/apache/thrift/lib/go/thrift"
)

type ChatRequest struct {
	Message   string `thrift:"message,1,required" json:"message"`
	SessionId string `thrift:"sessionId,2,optional" json:"sessionId"`
}

type ChatResponse struct {
	Response         string `thrift:"response,1,required" json:"response"`
	SessionId        string `thrift:"sessionId,2,optional" json:"sessionId"`
	Success          bool   `thrift:"success,3,optional" json:"success"`
	Error            string `thrift:"error,4,optional" json:"error"`
	PromptTokens     int    `thrift:"promptTokens,5,optional" json:"prompt_tokens"`
	CompletionTokens int    `thrift:"completionTokens,6,optional" json:"completion_tokens"`
	TotalTokens      int    `thrift:"totalTokens,7,optional" json:"total_tokens"`
}

type StreamChatResponse struct {
	Response         string `thrift:"response,1,required" json:"response"`
	SessionId        string `thrift:"sessionId,2,optional" json:"sessionId"`
	IsLast           bool   `thrift:"isLast,3,optional" json:"is_last"`
	Success          bool   `thrift:"success,4,optional" json:"success"`
	Error            string `thrift:"error,5,optional" json:"error"`
	PromptTokens     int    `thrift:"promptTokens,6,optional" json:"prompt_tokens"`
	CompletionTokens int    `thrift:"completionTokens,7,optional" json:"completion_tokens"`
	TotalTokens      int    `thrift:"totalTokens,8,optional" json:"total_tokens"`
}

type LdAgentService interface {
	Chat(ctx context.Context, request *ChatRequest) (*ChatResponse, error)
	StreamChat(ctx context.Context, request *ChatRequest, sender func(*StreamChatResponse) error) error
}

type ldAgentServiceProcessor struct {
	handler LdAgentService
}

func NewLdAgentServiceProcessor(handler LdAgentService) thrift.TProcessor {
	return &ldAgentServiceProcessor{handler: handler}
}

func (p *ldAgentServiceProcessor) ProcessorMap() map[string]thrift.TProcessorFunction {
	return nil
}

func (p *ldAgentServiceProcessor) AddToProcessorMap(_ string, _ thrift.TProcessorFunction) {
}

func (p *ldAgentServiceProcessor) Process(ctx context.Context, iprot, oprot thrift.TProtocol) (bool, thrift.TException) {
	name, _, seqid, err := iprot.ReadMessageBegin(ctx)
	if err != nil {
		return false, thrift.NewTApplicationException(thrift.INTERNAL_ERROR, err.Error())
	}

	switch name {
	case "chat":
		return p.processChat(ctx, seqid, iprot, oprot)
	case "streamChat":
		return p.processStreamChat(ctx, seqid, iprot, oprot)
	default:
		iprot.Skip(ctx, thrift.STRUCT)
		iprot.ReadMessageEnd(ctx)
		x := thrift.NewTApplicationException(thrift.UNKNOWN_METHOD, "Unknown function "+name)
		oprot.WriteMessageBegin(ctx, name, thrift.EXCEPTION, seqid)
		x.Write(ctx, oprot)
		oprot.WriteMessageEnd(ctx)
		oprot.Flush(ctx)
		return false, x
	}
}

func (p *ldAgentServiceProcessor) processChat(ctx context.Context, seqid int32, iprot, oprot thrift.TProtocol) (bool, thrift.TException) {
	var request ChatRequest
	if err := p.readChatRequest(ctx, iprot, &request); err != nil {
		return false, thrift.NewTApplicationException(thrift.INTERNAL_ERROR, err.Error())
	}

	response, err := p.handler.Chat(ctx, &request)
	if err != nil {
		x := thrift.NewTApplicationException(thrift.INTERNAL_ERROR, err.Error())
		oprot.WriteMessageBegin(ctx, "chat", thrift.EXCEPTION, seqid)
		x.Write(ctx, oprot)
		oprot.WriteMessageEnd(ctx)
		oprot.Flush(ctx)
		return false, x
	}

	oprot.WriteMessageBegin(ctx, "chat", thrift.REPLY, seqid)
	if err := p.writeChatResponse(ctx, oprot, response); err != nil {
		return false, thrift.NewTApplicationException(thrift.INTERNAL_ERROR, err.Error())
	}
	oprot.WriteMessageEnd(ctx)
	oprot.Flush(ctx)
	return true, nil
}

func (p *ldAgentServiceProcessor) processStreamChat(ctx context.Context, seqid int32, iprot, oprot thrift.TProtocol) (bool, thrift.TException) {
	var request ChatRequest
	if err := p.readChatRequest(ctx, iprot, &request); err != nil {
		return false, thrift.NewTApplicationException(thrift.INTERNAL_ERROR, err.Error())
	}

	sender := func(response *StreamChatResponse) error {
		oprot.WriteMessageBegin(ctx, "streamChat", thrift.REPLY, seqid)
		if err := p.writeStreamChatResponse(ctx, oprot, response); err != nil {
			return err
		}
		oprot.WriteMessageEnd(ctx)
		return oprot.Flush(ctx)
	}

	err := p.handler.StreamChat(ctx, &request, sender)
	if err != nil {
		x := thrift.NewTApplicationException(thrift.INTERNAL_ERROR, err.Error())
		oprot.WriteMessageBegin(ctx, "streamChat", thrift.EXCEPTION, seqid)
		x.Write(ctx, oprot)
		oprot.WriteMessageEnd(ctx)
		oprot.Flush(ctx)
		return false, x
	}

	return true, nil
}

func (p *ldAgentServiceProcessor) readChatRequest(ctx context.Context, iprot thrift.TProtocol, request *ChatRequest) error {
	_, err := iprot.ReadStructBegin(ctx)
	if err != nil {
		return err
	}

	for {
		_, fieldTypeId, fieldId, err := iprot.ReadFieldBegin(ctx)
		if err != nil {
			return err
		}
		if fieldTypeId == thrift.STOP {
			break
		}
		switch fieldId {
		case 1:
			if fieldTypeId != thrift.STRING {
				if err := iprot.Skip(ctx, fieldTypeId); err != nil {
					return err
				}
				continue
			}
			val, err := iprot.ReadString(ctx)
			if err != nil {
				return err
			}
			request.Message = val
		case 2:
			if fieldTypeId != thrift.STRING {
				if err := iprot.Skip(ctx, fieldTypeId); err != nil {
					return err
				}
				continue
			}
			val, err := iprot.ReadString(ctx)
			if err != nil {
				return err
			}
			request.SessionId = val
		default:
			if err := iprot.Skip(ctx, fieldTypeId); err != nil {
				return err
			}
		}
		if err := iprot.ReadFieldEnd(ctx); err != nil {
			return err
		}
	}

	if err := iprot.ReadStructEnd(ctx); err != nil {
		return err
	}

	return nil
}

func (p *ldAgentServiceProcessor) writeChatResponse(ctx context.Context, oprot thrift.TProtocol, response *ChatResponse) error {
	if err := oprot.WriteStructBegin(ctx, "chat_result"); err != nil {
		return err
	}

	if response.Response != "" {
		if err := oprot.WriteFieldBegin(ctx, "response", thrift.STRING, 1); err != nil {
			return err
		}
		if err := oprot.WriteString(ctx, response.Response); err != nil {
			return err
		}
		if err := oprot.WriteFieldEnd(ctx); err != nil {
			return err
		}
	}

	if response.SessionId != "" {
		if err := oprot.WriteFieldBegin(ctx, "sessionId", thrift.STRING, 2); err != nil {
			return err
		}
		if err := oprot.WriteString(ctx, response.SessionId); err != nil {
			return err
		}
		if err := oprot.WriteFieldEnd(ctx); err != nil {
			return err
		}
	}

	if err := oprot.WriteFieldBegin(ctx, "success", thrift.BOOL, 3); err != nil {
		return err
	}
	if err := oprot.WriteBool(ctx, response.Success); err != nil {
		return err
	}
	if err := oprot.WriteFieldEnd(ctx); err != nil {
		return err
	}

	if response.Error != "" {
		if err := oprot.WriteFieldBegin(ctx, "error", thrift.STRING, 4); err != nil {
			return err
		}
		if err := oprot.WriteString(ctx, response.Error); err != nil {
			return err
		}
		if err := oprot.WriteFieldEnd(ctx); err != nil {
			return err
		}
	}

	if err := oprot.WriteFieldStop(ctx); err != nil {
		return err
	}

	if err := oprot.WriteStructEnd(ctx); err != nil {
		return err
	}

	return nil
}

func (p *ldAgentServiceProcessor) writeStreamChatResponse(ctx context.Context, oprot thrift.TProtocol, response *StreamChatResponse) error {
	if err := oprot.WriteStructBegin(ctx, "streamChat_result"); err != nil {
		return err
	}

	if response.Response != "" {
		if err := oprot.WriteFieldBegin(ctx, "response", thrift.STRING, 1); err != nil {
			return err
		}
		if err := oprot.WriteString(ctx, response.Response); err != nil {
			return err
		}
		if err := oprot.WriteFieldEnd(ctx); err != nil {
			return err
		}
	}

	if response.SessionId != "" {
		if err := oprot.WriteFieldBegin(ctx, "sessionId", thrift.STRING, 2); err != nil {
			return err
		}
		if err := oprot.WriteString(ctx, response.SessionId); err != nil {
			return err
		}
		if err := oprot.WriteFieldEnd(ctx); err != nil {
			return err
		}
	}

	if err := oprot.WriteFieldBegin(ctx, "isLast", thrift.BOOL, 3); err != nil {
		return err
	}
	if err := oprot.WriteBool(ctx, response.IsLast); err != nil {
		return err
	}
	if err := oprot.WriteFieldEnd(ctx); err != nil {
		return err
	}

	if err := oprot.WriteFieldBegin(ctx, "success", thrift.BOOL, 4); err != nil {
		return err
	}
	if err := oprot.WriteBool(ctx, response.Success); err != nil {
		return err
	}
	if err := oprot.WriteFieldEnd(ctx); err != nil {
		return err
	}

	if response.Error != "" {
		if err := oprot.WriteFieldBegin(ctx, "error", thrift.STRING, 5); err != nil {
			return err
		}
		if err := oprot.WriteString(ctx, response.Error); err != nil {
			return err
		}
		if err := oprot.WriteFieldEnd(ctx); err != nil {
			return err
		}
	}

	if err := oprot.WriteFieldStop(ctx); err != nil {
		return err
	}

	if err := oprot.WriteStructEnd(ctx); err != nil {
		return err
	}

	return nil
}
