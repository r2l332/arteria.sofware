INSERT INTO arteria.communication_points (comm_point_id, name, direction, protocol, host, port, is_active, tunnel_enabled, tunnel_node_id, tunnel_local_port, config_json, created_at, updated_at)
VALUES (1d1d5033-90e5-11f1-a7f8-3ea3a77a153f, 'Darren Inbound (MLLP)', 'INPUT', 'MLLP', '0.0.0.0', 2575, true, true, d35f1d6b-90e2-11f1-a7f0-3ea3a77a153f, 2575, '{"description": "Darren test inbound via Capillary"}', toTimestamp(now()), toTimestamp(now()));

INSERT INTO arteria.communication_points (comm_point_id, name, direction, protocol, host, port, is_active, tunnel_enabled, tunnel_node_id, tunnel_local_port, config_json, created_at, updated_at)
VALUES (9a47e4d7-585e-449d-b1ce-990d8d6a849b, 'Darren Outbound (MLLP)', 'OUTPUT', 'MLLP', '0.0.0.0', 2576, true, true, d35f1d6b-90e2-11f1-a7f0-3ea3a77a153f, 2576, '{"description": "Darren test outbound via Capillary"}', toTimestamp(now()), toTimestamp(now()));

INSERT INTO arteria.routes (route_id, name, description, source_comm_point_id, dest_comm_point_id, source_topic, destination_topic, is_active, created_at, updated_at)
VALUES (18bf2cdb-90ea-11f1-8bb3-5a2741e57d0e, 'Darren Test Route', 'Darren test route for ADT^A28 registrations', 1d1d5033-90e5-11f1-a7f8-3ea3a77a153f, 9a47e4d7-585e-449d-b1ce-990d8d6a849b, 'ADT^A28', 'darren.registrations', true, toTimestamp(now()), toTimestamp(now()));

INSERT INTO arteria.filters (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active, created_at)
VALUES (d1000000-0000-0000-0000-000000000001, 18bf2cdb-90ea-11f1-8bb3-5a2741e57d0e, 'Change MSH-4 value', 'dotnet', 1,
$$using System;
using System.Text.Json;

var input = Console.In.ReadToEnd();
var doc = JsonDocument.Parse(input);
using var ms = new System.IO.MemoryStream();
using (var w = new Utf8JsonWriter(ms))
{
    w.WriteStartObject();
    foreach (var prop in doc.RootElement.EnumerateObject())
    {
        if (prop.Name == "sendingFacility")
            w.WriteString("sendingFacility", "DARREN_MODIFIED");
        else
            prop.WriteTo(w);
    }
    w.WriteEndObject();
}
Console.Write(System.Text.Encoding.UTF8.GetString(ms.ToArray()));$$,
'', true, toTimestamp(now()));

INSERT INTO arteria.filters_by_id (filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active)
VALUES (d1000000-0000-0000-0000-000000000001, 18bf2cdb-90ea-11f1-8bb3-5a2741e57d0e, 'Change MSH-4 value', 'dotnet', 1, '', '', true);
