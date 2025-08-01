// Code generated by protoc-gen-go-grpc. DO NOT EDIT.

package proto

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion6

// KeykammerServiceClient is the client API for KeykammerService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type KeykammerServiceClient interface {
	JoinRoom(ctx context.Context, in *JoinRequest, opts ...grpc.CallOption) (*JoinResponse, error)
	Chat(ctx context.Context, opts ...grpc.CallOption) (KeykammerService_ChatClient, error)
}

type keykammerServiceClient struct {
	cc *grpc.ClientConn
}

func NewKeykammerServiceClient(cc *grpc.ClientConn) KeykammerServiceClient {
	return &keykammerServiceClient{cc}
}

func (c *keykammerServiceClient) JoinRoom(ctx context.Context, in *JoinRequest, opts ...grpc.CallOption) (*JoinResponse, error) {
	out := new(JoinResponse)
	err := c.cc.Invoke(ctx, "/keykammer.KeykammerService/JoinRoom", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *keykammerServiceClient) Chat(ctx context.Context, opts ...grpc.CallOption) (KeykammerService_ChatClient, error) {
	stream, err := c.cc.NewStream(ctx, &_KeykammerService_serviceDesc.Streams[0], "/keykammer.KeykammerService/Chat", opts...)
	if err != nil {
		return nil, err
	}
	x := &keykammerServiceChatClient{stream}
	return x, nil
}

type KeykammerService_ChatClient interface {
	Send(*ChatMessage) error
	Recv() (*ChatMessage, error)
	grpc.ClientStream
}

type keykammerServiceChatClient struct {
	grpc.ClientStream
}

func (x *keykammerServiceChatClient) Send(m *ChatMessage) error {
	return x.ClientStream.SendMsg(m)
}

func (x *keykammerServiceChatClient) Recv() (*ChatMessage, error) {
	m := new(ChatMessage)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// KeykammerServiceServer is the server API for KeykammerService service.
// All implementations must embed UnimplementedKeykammerServiceServer
// for forward compatibility
type KeykammerServiceServer interface {
	JoinRoom(context.Context, *JoinRequest) (*JoinResponse, error)
	Chat(KeykammerService_ChatServer) error
	mustEmbedUnimplementedKeykammerServiceServer()
}

// UnimplementedKeykammerServiceServer must be embedded to have forward compatible implementations.
type UnimplementedKeykammerServiceServer struct {
}

func (UnimplementedKeykammerServiceServer) JoinRoom(context.Context, *JoinRequest) (*JoinResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method JoinRoom not implemented")
}
func (UnimplementedKeykammerServiceServer) Chat(KeykammerService_ChatServer) error {
	return status.Errorf(codes.Unimplemented, "method Chat not implemented")
}
func (UnimplementedKeykammerServiceServer) mustEmbedUnimplementedKeykammerServiceServer() {}

// UnsafeKeykammerServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to KeykammerServiceServer will
// result in compilation errors.
type UnsafeKeykammerServiceServer interface {
	mustEmbedUnimplementedKeykammerServiceServer()
}

func RegisterKeykammerServiceServer(s *grpc.Server, srv KeykammerServiceServer) {
	s.RegisterService(&_KeykammerService_serviceDesc, srv)
}

func _KeykammerService_JoinRoom_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(JoinRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(KeykammerServiceServer).JoinRoom(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/keykammer.KeykammerService/JoinRoom",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(KeykammerServiceServer).JoinRoom(ctx, req.(*JoinRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _KeykammerService_Chat_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(KeykammerServiceServer).Chat(&keykammerServiceChatServer{stream})
}

type KeykammerService_ChatServer interface {
	Send(*ChatMessage) error
	Recv() (*ChatMessage, error)
	grpc.ServerStream
}

type keykammerServiceChatServer struct {
	grpc.ServerStream
}

func (x *keykammerServiceChatServer) Send(m *ChatMessage) error {
	return x.ServerStream.SendMsg(m)
}

func (x *keykammerServiceChatServer) Recv() (*ChatMessage, error) {
	m := new(ChatMessage)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

var _KeykammerService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "keykammer.KeykammerService",
	HandlerType: (*KeykammerServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "JoinRoom",
			Handler:    _KeykammerService_JoinRoom_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "Chat",
			Handler:       _KeykammerService_Chat_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
	},
	Metadata: "proto/chat.proto",
}
