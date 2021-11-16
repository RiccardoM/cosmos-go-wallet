package cosmos

import (
	"context"
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"google.golang.org/grpc"

	"github.com/desmos-labs/cosmos-go-wallet/types"
)

// Client represents a Cosmos client that should be used to interact with a chain
type Client struct {
	Codec     codec.Codec
	RPCClient rpcclient.Client
	GRPCConn  *grpc.ClientConn
	txEncoder sdk.TxEncoder

	AuthClient authtypes.QueryClient
	GasPrice   sdk.DecCoin
}

// NewClient returns a new Client instance
func NewClient(config *types.ChainConfig, codec codec.Codec) (*Client, error) {
	client, err := client.NewClientFromNode(config.RPCAddr)
	if err != nil {
		return nil, err
	}

	grpcConn, err := CreateGrpcConnection(config.GRPCAddr)
	if err != nil {
		return nil, fmt.Errorf("error while creating a GRPC connection: %s", err)
	}

	gasPrice, err := sdk.ParseDecCoin(config.GasPrice)
	if err != nil {
		return nil, fmt.Errorf("error while parsing gas price: %s", err)
	}

	return &Client{
		Codec:      codec,
		RPCClient:  client,
		GRPCConn:   grpcConn,
		txEncoder:  tx.DefaultTxEncoder(),
		AuthClient: authtypes.NewQueryClient(grpcConn),
		GasPrice:   gasPrice,
	}, nil
}

// GetChainID returns the chain id associated to this client
func (c *Client) GetChainID() (string, error) {
	res, err := c.RPCClient.Status(context.Background())
	if err != nil {
		return "", fmt.Errorf("error while gestting chain id: %s", err)
	}

	return res.NodeInfo.Network, nil
}

// GetFeeDenom returns the denom used to pay for fees, based on the gas price inside the config
func (c *Client) GetFeeDenom() string {
	return c.GasPrice.Denom
}

// GetFees returns the fees that should be paid to perform a transaction with the given gas
func (c *Client) GetFees(gas int64) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(c.GasPrice.Denom, c.GasPrice.Amount.MulInt64(gas).TruncateInt()))
}

// GetAccount returns the details of the account having the given address reading it from the chain
func (c *Client) GetAccount(address string) (authtypes.AccountI, error) {
	res, err := c.AuthClient.Account(context.Background(), &authtypes.QueryAccountRequest{Address: address})
	if err != nil {
		return nil, err
	}

	var account authtypes.AccountI
	err = c.Codec.UnpackAny(res.Account, &account)
	if err != nil {
		return nil, err
	}

	return account, nil
}

// BroadcastTx allows to broadcast a transaction containing the given messages
func (c *Client) BroadcastTx(tx signing.Tx) (*sdk.TxResponse, error) {
	bytes, err := c.txEncoder(tx)
	if err != nil {
		return nil, err
	}

	res, err := c.RPCClient.BroadcastTxCommit(context.Background(), bytes)
	if err != nil {
		return nil, err
	}

	// Broadcast the transaction to a Tendermint node
	return sdk.NewResponseFormatBroadcastTxCommit(res), nil
}
