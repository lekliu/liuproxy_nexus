package encoding

import (
	"context"

	"google.golang.org/grpc"
)

func (c *gRPCServiceClient) TunCustomName(ctx context.Context, name, tun string, opts ...grpc.CallOption) (GRPCService_TunClient, error) {
	stream, err := c.cc.NewStream(ctx, &grpc.StreamDesc{StreamName: tun, ServerStreams: true, ClientStreams: true}, "/"+name+"/"+tun, opts...)
	if err != nil {
		return nil, err
	}
	x := &gRPCServiceTunClient{stream}
	return x, nil
}

func (c *gRPCServiceClient) TunMultiCustomName(ctx context.Context, name, tunMulti string, opts ...grpc.CallOption) (GRPCService_TunMultiClient, error) {
	stream, err := c.cc.NewStream(ctx, &grpc.StreamDesc{StreamName: tunMulti, ServerStreams: true, ClientStreams: true}, "/"+name+"/"+tunMulti, opts...)
	if err != nil {
		return nil, err
	}
	x := &gRPCServiceTunMultiClient{stream}
	return x, nil
}

type GRPCServiceClientX interface {
	TunCustomName(ctx context.Context, name, tun string, opts ...grpc.CallOption) (GRPCService_TunClient, error)
	TunMultiCustomName(ctx context.Context, name, tunMulti string, opts ...grpc.CallOption) (GRPCService_TunMultiClient, error)
	Tun(ctx context.Context, opts ...grpc.CallOption) (GRPCService_TunClient, error)
	TunMulti(ctx context.Context, opts ...grpc.CallOption) (GRPCService_TunMultiClient, error)
}

//func RegisterGRPCServiceServerX(s *grpc.Server, srv GRPCServiceServer, name, tun, tunMulti string) {
//	desc := ServerDesc(name, tun, tunMulti)
//	s.RegisterService(&desc, srv)
//}
