package cf

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/option"
	"github.com/cloudflare/cloudflare-go/v6/zero_trust"
)

type Tunnel struct {
	// Cloudflare Tunnel ID
	ID string
	// Cloudflare Tunnel name
	Name string
}

// TunnelClientInterface defines the interface for Cloudflare Tunnel operations
type TunnelClientInterface interface {
	NewTunnel(ctx context.Context, params TunnelNewParams) (*Tunnel, error)
	FindTunnel(ctx context.Context, params FindTunnelParams) (*Tunnel, error)
	DeleteTunnel(ctx context.Context, params DeleteTunnelParams) error
	GetTunnelToken(ctx context.Context, params GetTunnelTokenParams) (*string, error)
}

func NewTunnel(id string, name string) *Tunnel {
	return &Tunnel{ID: id, Name: name}
}

type TunnelClient struct {
	TunnelClientInterface

	client    *cloudflare.Client
	accountID string
	// This prefix is automatically prepended to the tunnel name
	// when creating, searching and deleting tunnels
	// to avoid conflicts with other tunnels
	tunnelNamePrefix string
}

type TunnelClientNewParams struct {
	AccountID        string
	APIToken         string
	TunnelNamePrefix string
}

func NewTunnelClient(params TunnelClientNewParams) *TunnelClient {
	opts := []option.RequestOption{
		option.WithAPIToken(params.APIToken),
	}
	client := cloudflare.NewClient(opts...)
	return &TunnelClient{client: client, accountID: params.AccountID, tunnelNamePrefix: params.TunnelNamePrefix}
}

func (t *TunnelClient) nameWithPrefix(name string) string {
	return fmt.Sprintf("%s%s", t.tunnelNamePrefix, name)
}

type TunnelNewParams struct {
	// This name must be unique within the alive (not-deleted) tunnels
	// required
	Name string
	// Tunnel secret encoded in base64
	// required
	TunnelSecret string
}

// If the tunnel with the same name already exists (alive/not-deleted), the following error will be returned:
//
//	409 Conflict
//	  code: 1013
//	  message:
//	    You already have a tunnel with this name.
//	    Delete the existing tunnel, or choose a different name for your new tunnel
func (t *TunnelClient) NewTunnel(ctx context.Context, params TunnelNewParams) (*Tunnel, error) {
	// params validation
	if params.Name == "" {
		return nil, errors.New("param 'Name' is required")
	}
	if params.TunnelSecret == "" {
		return nil, errors.New("param 'TunnelSecret' is required")
	}

	name := t.nameWithPrefix(params.Name)
	tunnel, err := t.client.ZeroTrust.Tunnels.Cloudflared.New(
		ctx,
		zero_trust.TunnelCloudflaredNewParams{
			AccountID:    cloudflare.F(t.accountID),
			Name:         cloudflare.F(name),
			ConfigSrc:    cloudflare.F(zero_trust.TunnelCloudflaredNewParamsConfigSrcLocal),
			TunnelSecret: cloudflare.F(params.TunnelSecret),
		},
	)
	if err != nil {
		return nil, err
	}

	return NewTunnel(tunnel.ID, tunnel.Name), nil
}

type FindTunnelParams struct {
	Name string
}

var (
	ErrFindTunnelNotFound = errors.New("specified tunnel does not exist")
)

func (t *TunnelClient) FindTunnel(ctx context.Context, params FindTunnelParams) (*Tunnel, error) {
	name := t.nameWithPrefix(params.Name)
	tunnels, err := t.client.ZeroTrust.Tunnels.Cloudflared.List(
		ctx,
		zero_trust.TunnelCloudflaredListParams{
			AccountID: cloudflare.F(t.accountID),
			Name:      cloudflare.F(name),
			IsDeleted: cloudflare.F(false),
		},
	)
	if err != nil {
		return nil, err
	}
	// filter tunnels by name
	var filteredTunnels []*Tunnel
	for _, tunnel := range tunnels.Result {
		if tunnel.Name == name {
			filteredTunnels = append(filteredTunnels, NewTunnel(tunnel.ID, tunnel.Name))
		}
	}
	switch len(filteredTunnels) {
	case 0:
		return nil, ErrFindTunnelNotFound
	case 1:
		return filteredTunnels[0], nil
	default: // Logically not occurring but just in case to prevent panic
		return nil, fmt.Errorf("multiple tunnels found with the same name")
	}
}

type DeleteTunnelParams struct {
	TunnelID string
}

// 'DeleteTunnel' is idempotent
func (t *TunnelClient) DeleteTunnel(ctx context.Context, params DeleteTunnelParams) error {
	// 'Delete' is idempotent
	_, err := t.client.ZeroTrust.Tunnels.Cloudflared.Delete(
		ctx,
		params.TunnelID,
		zero_trust.TunnelCloudflaredDeleteParams{
			AccountID: cloudflare.F(t.accountID),
		},
	)
	if err != nil {
		return err
	}
	return nil
}

type GetTunnelTokenParams struct {
	TunnelID string
}

// Returns Cloudflare Tunnel token encoded in base64
// that decoded format is `{"a": 'xxxx', "t": 'yyyy', "s": 'zzzz'}` json string
func (t *TunnelClient) GetTunnelToken(ctx context.Context, params GetTunnelTokenParams) (*string, error) {
	tunnelToken, err := t.client.ZeroTrust.Tunnels.Cloudflared.Token.Get(
		ctx,
		params.TunnelID,
		zero_trust.TunnelCloudflaredTokenGetParams{
			AccountID: cloudflare.F(t.accountID),
		},
	)
	if err != nil {
		return nil, err
	}
	return tunnelToken, nil
}
