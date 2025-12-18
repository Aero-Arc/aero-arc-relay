package controlpb

import (
	"context"

	"google.golang.org/grpc"
)

// GetStatusRequest is an empty request message for GetStatus.
type GetStatusRequest struct{}

// GetStatusResponse describes high-level relay status.
type GetStatusResponse struct {
	Version       string `json:"version"`
	State         string `json:"state"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

// ListEndpointsRequest is an empty request message for ListEndpoints.
type ListEndpointsRequest struct{}

// EndpointInfo describes a single configured MAVLink endpoint.
type EndpointInfo struct {
	Name     string `json:"name"`
	DroneID  string `json:"drone_id"`
	Protocol string `json:"protocol"`
	Port     int32  `json:"port"`
}

// ListEndpointsResponse wraps a list of endpoint descriptions.
type ListEndpointsResponse struct {
	Endpoints []*EndpointInfo `json:"endpoints"`
}

// RelayControlServer defines the control-plane RPCs for the relay.
type RelayControlServer interface {
	GetStatus(context.Context, *GetStatusRequest) (*GetStatusResponse, error)
	ListEndpoints(context.Context, *ListEndpointsRequest) (*ListEndpointsResponse, error)
}

// AgentGatewayServer is a placeholder for the existing AgentGateway service.
// It is intentionally minimal; methods can be added as the gateway API evolves.
type AgentGatewayServer interface{}

// RegisterRelayControlServer registers the RelayControl service with a gRPC server.
func RegisterRelayControlServer(s grpc.ServiceRegistrar, srv RelayControlServer) {
	s.RegisterService(&RelayControl_ServiceDesc, srv)
}

// RegisterAgentGatewayServer registers the AgentGateway service with a gRPC server.
// For now it only declares the service without any methods.
func RegisterAgentGatewayServer(s grpc.ServiceRegistrar, srv AgentGatewayServer) {
	s.RegisterService(&AgentGateway_ServiceDesc, srv)
}

// RelayControl_ServiceDesc describes the RelayControl gRPC service.
var RelayControl_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "aeroarc.relay.v1.RelayControl",
	HandlerType: (*RelayControlServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "GetStatus",
			Handler:    _RelayControl_GetStatus_Handler,
		},
		{
			MethodName: "ListEndpoints",
			Handler:    _RelayControl_ListEndpoints_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "relay_control",
}

// AgentGateway_ServiceDesc describes the AgentGateway gRPC service.
// It is defined with no methods for now to satisfy registration requirements.
var AgentGateway_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "aeroarc.relay.v1.AgentGateway",
	HandlerType: (*AgentGatewayServer)(nil),
	Methods:     []grpc.MethodDesc{},
	Streams:     []grpc.StreamDesc{},
	Metadata:    "agent_gateway",
}

func _RelayControl_GetStatus_Handler(
	srv interface{},
	ctx context.Context,
	dec func(interface{}) error,
	interceptor grpc.UnaryServerInterceptor,
) (interface{}, error) {
	in := new(GetStatusRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RelayControlServer).GetStatus(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/aeroarc.relay.v1.RelayControl/GetStatus",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RelayControlServer).GetStatus(ctx, req.(*GetStatusRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _RelayControl_ListEndpoints_Handler(
	srv interface{},
	ctx context.Context,
	dec func(interface{}) error,
	interceptor grpc.UnaryServerInterceptor,
) (interface{}, error) {
	in := new(ListEndpointsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RelayControlServer).ListEndpoints(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/aeroarc.relay.v1.RelayControl/ListEndpoints",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RelayControlServer).ListEndpoints(ctx, req.(*ListEndpointsRequest))
	}
	return interceptor(ctx, in, info, handler)
}


