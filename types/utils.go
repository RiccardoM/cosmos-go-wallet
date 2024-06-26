package types

import (
	"crypto/tls"
	"regexp"
	"strings"

	"github.com/cosmos/cosmos-sdk/codec"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/riccardom/cosmos-go-wallet/gprc"
)

var (
	HTTPProtocols = regexp.MustCompile("https?://")
)

// CreateGrpcConnection creates a new gRPC client connection from the given configuration
func CreateGrpcConnection(config *ChainConfig, codec codec.Codec) (grpc.ClientConnInterface, error) {
	// Get the gRPC address
	grpcAddress := config.GRPCAddr

	// If the gRPC address is not set, we should use gRPC-over-RPC
	if grpcAddress == "" {
		return gprc.NewConnection(config.RPCAddr, codec)
	}

	// Create the gRPC connection
	var transportCredentials credentials.TransportCredentials
	if strings.HasPrefix(grpcAddress, "https") {
		transportCredentials = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	} else {
		transportCredentials = insecure.NewCredentials()
	}

	// Remove the HTTP protocol from the address
	grpcAddress = HTTPProtocols.ReplaceAllString(grpcAddress, "")

	// Create the gRPC connection
	return grpc.Dial(grpcAddress, grpc.WithTransportCredentials(transportCredentials))
}
