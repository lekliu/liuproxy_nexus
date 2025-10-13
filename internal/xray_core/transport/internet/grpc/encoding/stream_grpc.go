package encoding

import (
	"context"
	"google.golang.org/grpc"
)

const _ = grpc.SupportPackageIsVersion7

const (
	GRPCService_Tun_FullMethodName      = "/xray.transport.internet.grpc.encoding.GRPCService/Tun"
	GRPCService_TunMulti_FullMethodName = "/xray.transport.internet.grpc.encoding.GRPCService/TunMulti"
)

// GRPCServiceClient is the client API for GRPCService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type GRPCServiceClient interface {
	Tun(ctx context.Context, opts ...grpc.CallOption) (GRPCService_TunClient, error)
	TunMulti(ctx context.Context, opts ...grpc.CallOption) (GRPCService_TunMultiClient, error)
}

type gRPCServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewGRPCServiceClient(cc grpc.ClientConnInterface) GRPCServiceClient {
	return &gRPCServiceClient{cc}
}

func (c *gRPCServiceClient) Tun(ctx context.Context, opts ...grpc.CallOption) (GRPCService_TunClient, error) {
	stream, err := c.cc.NewStream(ctx, &grpc.StreamDesc{StreamName: "Tun", ServerStreams: true, ClientStreams: true}, GRPCService_Tun_FullMethodName, opts...)
	if err != nil {
		return nil, err
	}
	x := &gRPCServiceTunClient{stream}
	return x, nil
}

type GRPCService_TunClient interface {
	Send(*Hunk) error
	Recv() (*Hunk, error)
	grpc.ClientStream
}

type gRPCServiceTunClient struct {
	grpc.ClientStream
}

func (x *gRPCServiceTunClient) Send(m *Hunk) error {
	return x.ClientStream.SendMsg(m)
}

func (x *gRPCServiceTunClient) Recv() (*Hunk, error) {
	m := new(Hunk)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *gRPCServiceClient) TunMulti(ctx context.Context, opts ...grpc.CallOption) (GRPCService_TunMultiClient, error) {
	stream, err := c.cc.NewStream(ctx, &grpc.StreamDesc{StreamName: "TunMulti", ServerStreams: true, ClientStreams: true}, GRPCService_TunMulti_FullMethodName, opts...)
	if err != nil {
		return nil, err
	}
	x := &gRPCServiceTunMultiClient{stream}
	return x, nil
}

type GRPCService_TunMultiClient interface {
	Send(*MultiHunk) error
	Recv() (*MultiHunk, error)
	grpc.ClientStream
}

type gRPCServiceTunMultiClient struct {
	grpc.ClientStream
}

func (x *gRPCServiceTunMultiClient) Send(m *MultiHunk) error {
	return x.ClientStream.SendMsg(m)
}

func (x *gRPCServiceTunMultiClient) Recv() (*MultiHunk, error) {
	m := new(MultiHunk)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}
