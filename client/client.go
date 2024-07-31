package client

import (
	"context"
	"fmt"
	"math"
	"strings"

	rpcclient "github.com/cometbft/cometbft/rpc/client"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	sdkclient "github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"google.golang.org/grpc"

	"github.com/riccardom/cosmos-go-wallet/types"
)

// Client represents a Cosmos client that should be used to interact with a chain
type Client struct {
	prefix string

	rpcClient rpcclient.Client
	grpcConn  grpc.ClientConnInterface
	codec     codec.Codec
	txConfig  sdkclient.TxConfig
	txEncoder sdk.TxEncoder

	authClient authtypes.QueryClient
	bankClient banktypes.QueryClient
	txClient   sdktx.ServiceClient

	gasPrice      sdk.DecCoin
	gasAdjustment float64
}

// NewClient allows to build a new Client instance
func NewClient(
	bech32Prefix string,
	gasPrice sdk.DecCoin,
	rpcClient *rpchttp.HTTP,
	grpcConn grpc.ClientConnInterface,
	txConfig sdkclient.TxConfig,
	codec codec.Codec,
) *Client {
	return &Client{
		prefix: bech32Prefix,

		codec:     codec,
		rpcClient: rpcClient,
		grpcConn:  grpcConn,
		txEncoder: tx.DefaultTxEncoder(),
		txConfig:  txConfig,

		authClient: authtypes.NewQueryClient(grpcConn),
		bankClient: banktypes.NewQueryClient(grpcConn),
		txClient:   sdktx.NewServiceClient(grpcConn),

		gasPrice:      gasPrice,
		gasAdjustment: 1.5,
	}
}

// NewClientFromConfig returns a new Client instance based on the given configuration
func NewClientFromConfig(config *types.ChainConfig, txConfig sdkclient.TxConfig, codec codec.Codec) (*Client, error) {
	rpcClient, err := sdkclient.NewClientFromNode(config.RPCAddr)
	if err != nil {
		return nil, err
	}

	grpcConn, err := types.CreateGrpcConnection(config, codec)
	if err != nil {
		return nil, fmt.Errorf("error while creating a GRPC connection: %s", err)
	}

	gasPrice, err := sdk.ParseDecCoin(config.GasPrice)
	if err != nil {
		return nil, fmt.Errorf("error while parsing gas price: %s", err)
	}

	// Build the client
	cosmosClient := NewClient(config.Bech32Prefix, gasPrice, rpcClient, grpcConn, txConfig, codec)

	// Set the options based on the config
	cosmosClient = cosmosClient.WithGasAdjustment(config.GasAdjustment)

	return cosmosClient, nil
}

// --------------------------------------------------------------------------------------------------------------------

// WithGasAdjustment allows to set the gas adjustment factor to be used when simulating transactions
func (c *Client) WithGasAdjustment(gasAdjustment float64) *Client {
	c.gasAdjustment = gasAdjustment
	return c
}

// --------------------------------------------------------------------------------------------------------------------

// GetTxConfig returns the transaction configuration associated to this client
func (c *Client) GetTxConfig() sdkclient.TxConfig {
	return c.txConfig
}

// GetAccountPrefix returns the account prefix to be used when serializing addresses as Bech32
func (c *Client) GetAccountPrefix() string {
	return c.prefix
}

// ParseAddress parses the given address as an sdk.AccAddress instance
func (c *Client) ParseAddress(address string) (sdk.AccAddress, error) {
	if len(strings.TrimSpace(address)) == 0 {
		return nil, fmt.Errorf("empty address string is not allowed")
	}

	prefix, bz, err := bech32.DecodeAndConvert(address)
	if err != nil {
		return nil, err
	}

	if prefix != c.GetAccountPrefix() {
		return nil, fmt.Errorf("invalid bech32 prefix: exptected %s, got %s", c.GetAccountPrefix(), prefix)
	}

	err = sdk.VerifyAddressFormat(bz)
	if err != nil {
		return nil, err
	}

	return bz, nil
}

// GetChainID returns the chain id associated to this client
func (c *Client) GetChainID() (string, error) {
	res, err := c.rpcClient.Status(context.Background())
	if err != nil {
		return "", fmt.Errorf("error while getting chain id: %s", err)
	}

	return res.NodeInfo.Network, nil
}

// GetFeeDenom returns the denom used to pay for fees, based on the gas price inside the config
func (c *Client) GetFeeDenom() string {
	return c.gasPrice.Denom
}

// GetFees returns the fees that should be paid to perform a transaction with the given gas
func (c *Client) GetFees(gas int64) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(c.gasPrice.Denom, c.gasPrice.Amount.MulInt64(gas).Ceil().RoundInt()))
}

// GetAccount returns the details of the account having the given address reading it from the chain
func (c *Client) GetAccount(address string) (sdk.AccountI, error) {
	res, err := c.authClient.Account(context.Background(), &authtypes.QueryAccountRequest{Address: address})
	if err != nil {
		return nil, err
	}

	var account sdk.AccountI
	err = c.codec.UnpackAny(res.Account, &account)
	if err != nil {
		return nil, err
	}

	return account, nil
}

// GetBalances returns the balances of the account having the given address
func (c *Client) GetBalances(address string) (sdk.Coins, error) {
	res, err := c.bankClient.AllBalances(context.Background(), &banktypes.QueryAllBalancesRequest{Address: address})
	if err != nil {
		return nil, err
	}

	return res.Balances, nil
}

// --------------------------------------------------------------------------------------------------------------------

// SimulateTx simulates the execution of the given transaction, and returns the adjusted
// amount of gas that should be used in order to properly execute it
func (c *Client) SimulateTx(tx signing.Tx) (uint64, error) {
	bytes, err := c.txEncoder(tx)
	if err != nil {
		return 0, err
	}

	simRes, err := c.txClient.Simulate(context.Background(), &sdktx.SimulateRequest{
		TxBytes: bytes,
	})
	if err != nil {
		return 0, err
	}

	return uint64(math.Ceil(c.gasAdjustment * float64(simRes.GasInfo.GasUsed))), nil
}

// BroadcastTxAsync allows to broadcast a transaction containing the given messages using the sync method
func (c *Client) BroadcastTxAsync(tx signing.Tx) (*sdk.TxResponse, error) {
	bytes, err := c.txEncoder(tx)
	if err != nil {
		return nil, err
	}

	res, err := c.rpcClient.BroadcastTxAsync(context.Background(), bytes)
	if err != nil {
		return nil, err
	}

	// Broadcast the transaction to a Tendermint node
	return sdk.NewResponseFormatBroadcastTx(res), nil
}

// BroadcastTxSync allows to broadcast a transaction containing the given messages using the sync method
func (c *Client) BroadcastTxSync(tx signing.Tx) (*sdk.TxResponse, error) {
	bytes, err := c.txEncoder(tx)
	if err != nil {
		return nil, err
	}

	res, err := c.rpcClient.BroadcastTxSync(context.Background(), bytes)
	if err != nil {
		return nil, err
	}

	// Broadcast the transaction to a Tendermint node
	return sdk.NewResponseFormatBroadcastTx(res), nil
}

// BroadcastTxCommit allows to broadcast a transaction containing the given messages using the commit method
func (c *Client) BroadcastTxCommit(tx signing.Tx) (*sdk.TxResponse, error) {
	bytes, err := c.txEncoder(tx)
	if err != nil {
		return nil, err
	}

	res, err := c.rpcClient.BroadcastTxCommit(context.Background(), bytes)
	if err != nil {
		return nil, err
	}

	// Broadcast the transaction to a Tendermint node
	return NewResponseFormatBroadcastTxCommit(res), nil
}
