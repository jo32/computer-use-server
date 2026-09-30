package server

import (
	"computer-use-server/internal/harness"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type rpc struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (s *Server) mcp(w http.ResponseWriter, r *http.Request) {
	var req rpc
	if !decode(w, r, &req) {
		return
	}
	reply := func(result any) { write(w, map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}) }
	fail := func(code int, message string) {
		id := req.ID
		if len(id) == 0 {
			id = json.RawMessage(`null`)
		}
		write(w, map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		fail(-32600, "Invalid Request")
		return
	}
	if req.Method == "initialize" {
		if len(req.ID) == 0 {
			fail(-32600, "initialize needs an id")
			return
		}
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
			ClientInfo      struct {
				Name string `json:"name"`
			} `json:"clientInfo"`
		}
		if json.Unmarshal(req.Params, &p) != nil {
			fail(-32602, "Invalid initialize params")
			return
		}
		version := "2025-06-18"
		if p.ProtocolVersion == "2025-03-26" {
			version = p.ProtocolVersion
		}
		id := harness.ID()
		s.mu.Lock()
		if s.sessions == nil {
			s.sessions = map[string]mcpSession{}
		}
		for key, v := range s.sessions {
			if time.Since(v.At) > 24*time.Hour {
				delete(s.sessions, key)
			}
		}
		if len(s.sessions) >= 256 {
			s.mu.Unlock()
			problem(w, 429, fmt.Errorf("MCP session limit reached"))
			return
		}
		s.sessions[id] = mcpSession{Client: p.ClientInfo.Name, At: time.Now()}
		s.mu.Unlock()
		w.Header().Set("Mcp-Session-Id", id)
		reply(map[string]any{"protocolVersion": version, "serverInfo": map[string]string{"name": "local-agent-adapter", "version": "0.4.0"}, "capabilities": map[string]any{"tools": map[string]bool{"listChanged": false}}, "instructions": "Use tools/list to discover tools, or call help with {} for currently available tools and complete parameter definitions. Call help with name for one tool, or include_disabled=true to inspect blocked tools. Permissions are controlled from the local dashboard. Always capture a fresh screenshot before coordinate actions. exec_command executes on the host without an OS sandbox."})
		return
	}
	sid := r.Header.Get("Mcp-Session-Id")
	s.mu.Lock()
	session, ok := s.sessions[sid]
	s.mu.Unlock()
	if !ok || time.Since(session.At) > 24*time.Hour {
		problem(w, 404, fmt.Errorf("MCP session expired; initialize again"))
		return
	}
	if len(req.ID) == 0 {
		w.WriteHeader(202)
		return
	}
	switch req.Method {
	case "ping":
		reply(map[string]any{})
	case "tools/list":
		out := []map[string]any{}
		for _, t := range s.Registry.Specs() {
			annotations := t.Annotations
			if annotations == nil {
				annotations = map[string]any{"readOnlyHint": !t.Mutating, "destructiveHint": t.Mutating, "openWorldHint": t.Category != "files"}
			}
			tool := map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.InputSchema, "annotations": annotations}
			if t.OutputSchema != nil {
				tool["outputSchema"] = t.OutputSchema
			}
			out = append(out, tool)
		}
		reply(map[string]any{"tools": out})
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
			fail(-32602, "Invalid tool call")
			return
		}
		if len(p.Arguments) == 0 {
			p.Arguments = json.RawMessage(`{}`)
		}
		out, call, err := s.Registry.Invoke(r.Context(), p.Name, harness.Invocation{Session: sid, Client: session.Client, Arguments: p.Arguments})
		if out.MCPResult != nil {
			// Preserve content blocks, structuredContent and isError exactly.
			reply(out.MCPResult)
			return
		}
		content := []map[string]any{}
		if value, ok := out.Value.(map[string]any); ok {
			if shot, ok := value["screenshot"].(string); ok {
				content = append(content, map[string]any{"type": "image", "mimeType": "image/jpeg", "data": strings.TrimPrefix(shot, "data:image/jpeg;base64,")})
				delete(value, "screenshot")
			}
		}
		b, _ := json.Marshal(map[string]any{"call_id": call.ID, "result": out.Value, "error": call.Error})
		content = append(content, map[string]any{"type": "text", "text": string(b)})
		reply(map[string]any{"content": content, "isError": err != nil})
	default:
		fail(-32601, "Method not found")
	}
}
func (s *Server) openapi(w http.ResponseWriter, r *http.Request) {
	paths := map[string]any{}
	for _, tool := range s.Registry.Specs() {
		paths["/api/v1/tools/"+tool.Name] = map[string]any{"post": map[string]any{"operationId": tool.Name, "summary": tool.Description, "tags": []string{tool.Category}, "parameters": []map[string]any{{"name": "X-Session-ID", "in": "header", "schema": map[string]string{"type": "string"}}}, "requestBody": map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": tool.InputSchema}}}, "responses": map[string]any{"200": map[string]any{"description": "Tool result, call_id and status"}, "422": map[string]any{"description": "Tool error"}, "423": map[string]any{"description": "Capability disabled or paused"}}}}
	}
	write(w, map[string]any{"openapi": "3.1.0", "info": map[string]string{"title": "Local Agent Adapter", "version": "0.4.0"}, "paths": paths, "servers": []map[string]string{{"url": "/" + s.AccessPath, "description": "Current run access path; regenerated on restart"}}})
}
