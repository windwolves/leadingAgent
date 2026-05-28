namespace go ldagent
namespace java com.example.ldagent
namespace py ldagent
namespace js ldagent

struct ChatRequest {
    1: required string message,
    2: optional string sessionId,
}

struct ChatResponse {
    1: required string response,
    2: optional string sessionId,
    3: optional bool success,
    4: optional string error,
    5: optional i32 promptTokens,
    6: optional i32 completionTokens,
    7: optional i32 totalTokens,
}

struct StreamChatResponse {
    1: required string response,
    2: optional string sessionId,
    3: optional bool isLast,
    4: optional bool success,
    5: optional string error,
    6: optional i32 promptTokens,
    7: optional i32 completionTokens,
    8: optional i32 totalTokens,
}

service LdAgentService {
    ChatResponse chat(1: ChatRequest request),
    StreamChatResponse streamChat(1: ChatRequest request),
}
