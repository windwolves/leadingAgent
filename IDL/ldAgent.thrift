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
}

service LdAgentService {
    ChatResponse chat(1: ChatRequest request),
}
