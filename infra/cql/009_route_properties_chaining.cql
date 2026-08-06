-- Route default properties: injected into every message before the filter chain
ALTER TABLE arteria.routes ADD default_properties text;

-- Connector filter config: stored in filters.config_json with filter_type = 'connector'
-- config_json schema for connector filters:
-- {
--   "connector_type": "HTTP" | "MLLP",
--   "url": "http://example.com/api/lookup",
--   "method": "POST",
--   "headers": {"Content-Type": "application/json"},
--   "timeout_ms": 5000,
--   "response_property": "api_response",
--   "response_status_property": "api_status",
--   "body_template": "{{.RawPayload}}"
-- }

-- Route chaining: next_route_id allows a route to forward to another route after completion
ALTER TABLE arteria.routes ADD next_route_id uuid;
