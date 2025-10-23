package cf

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/option"
	"github.com/cloudflare/cloudflare-go/v6/zero_trust"
	yamlGoYaml "go.yaml.in/yaml/v4"
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

	name := t.nameWithPrefix(params.Name)
	tunnel, err := t.client.ZeroTrust.Tunnels.Cloudflared.New(
		ctx,
		zero_trust.TunnelCloudflaredNewParams{
			AccountID: cloudflare.F(t.accountID),
			Name:      cloudflare.F(name),
			ConfigSrc: cloudflare.F(zero_trust.TunnelCloudflaredNewParamsConfigSrcCloudflare),
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

// FindTunnel searches for a tunnel by name
//
// ## Errors
//
// - `ErrFindTunnelNotFound`
//   - If the specified named tunnel does not exist, this error will be returned
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

// type TunnelToken struct {
// 	// Account ID
// 	AccountTag string `json:"AccountTag"`
// 	// Tunnel ID
// 	TunnelID   string `json:"TunnelID"`
// 	// secret (base64 encoded)
// 	TunnelSecret     string `json:"TunnelSecret"`
// }

// Returns Cloudflare Tunnel token encoded in base64
// that decoded format is `{"a": 'xxxx', "t": 'yyyy', "s": 'zzzz'}` json string
func (t *TunnelClient) GetTunnelToken(ctx context.Context, params GetTunnelTokenParams) (*string, error) {
	encodedTunnelToken, err := t.client.ZeroTrust.Tunnels.Cloudflared.Token.Get(
		ctx,
		params.TunnelID,
		zero_trust.TunnelCloudflaredTokenGetParams{
			AccountID: cloudflare.F(t.accountID),
		},
	)
	if err != nil {
		return nil, err
	}
	// decodedTunnelToken, err := base64.StdEncoding.DecodeString(*encodedTunnelToken)
	// if err != nil {
	// 	return nil, fmt.Errorf("decoding tunnel token: %w", err)
	// }

	// var tunnelToken TunnelToken
	// if err := json.Unmarshal(decodedTunnelToken, &tunnelToken); err != nil {
	// 	return nil, fmt.Errorf("unmarshalling tunnel token: %w", err)
	// }
	return encodedTunnelToken, nil
}

// The tunnel configuration and ingress rules.
type TunnelConfiguration struct {
	// List of public hostname definitions. At least one ingress rule needs to be
	// defined for the tunnel.
	Ingress []TunnelConfigurationIngress
	// Configuration parameters for the public hostname specific connection settings
	// between cloudflared and origin server.
	OriginRequest TunnelConfigurationOriginRequest `json:"originRequest"`
}

func (t *TunnelConfiguration) ToRequestFormat() *zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfig {
	value := zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfig{}
	isZeroValue := true
	if len(t.Ingress) > 0 {
		isZeroValue = false
		ingress := make([]zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress, len(t.Ingress))
		for _, ing := range t.Ingress {
			if ing := ing.ToRequestFormat(); ing != nil {
				ingress = append(ingress, *ing)
			}
		}
		value.Ingress = cloudflare.F(ingress)
	}
	if originRequest := t.OriginRequest.ToRequestFormat(); originRequest != nil {
		value.OriginRequest = cloudflare.F(*originRequest)
		isZeroValue = false
	}
	if isZeroValue {
		return nil
	}
	return &value
}

// Public hostname
type TunnelConfigurationIngress struct {
	// Public hostname for this service.
	Hostname string `json:"hostname"`
	// Protocol and address of destination server. Supported protocols: http://,
	// https://, unix://, tcp://, ssh://, rdp://, unix+tls://, smb://. Alternatively
	// can return a HTTP status code http_status:[code] e.g. 'http_status:404'.
	Service string `json:"service"`
	// Configuration parameters for the public hostname specific connection settings
	// between cloudflared and origin server.
	OriginRequest TunnelConfigurationIngressOriginRequest `json:"originRequest"`
	// Requests with this path route to this public hostname.
	Path string `json:"path"`
}

func (t *TunnelConfigurationIngress) ToRequestFormat() *zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress {
	value := zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress{}
	isZeroValue := true
	if t.Hostname != "" {
		value.Hostname = cloudflare.F(t.Hostname)
		isZeroValue = false
	}
	if t.Service != "" {
		value.Service = cloudflare.F(t.Service)
		isZeroValue = false
	}
	if originRequest := t.OriginRequest.ToRequestFormat(); originRequest != nil {
		value.OriginRequest = cloudflare.F(*originRequest)
		isZeroValue = false
	}
	if t.Path != "" {
		value.Path = cloudflare.F(t.Path)
		isZeroValue = false
	}
	if isZeroValue {
		return nil
	}
	return &value
}

// Configuration parameters for the public hostname specific connection settings
// between cloudflared and origin server.
type TunnelConfigurationOriginRequest struct {
	// For all L7 requests to this hostname, cloudflared will validate each request's
	// Cf-Access-Jwt-Assertion request header.
	Access TunnelConfigurationOriginRequestAccess `json:"access"`
	// Path to the certificate authority (CA) for the certificate of your origin. This
	// option should be used only if your certificate is not signed by Cloudflare.
	CAPool string `json:"caPool"`
	// Timeout for establishing a new TCP connection to your origin server. This
	// excludes the time taken to establish TLS, which is controlled by tlsTimeout.
	ConnectTimeout int64 `json:"connectTimeout"`
	// Disables chunked transfer encoding. Useful if you are running a WSGI server.
	DisableChunkedEncoding bool `json:"disableChunkedEncoding"`
	// Attempt to connect to origin using HTTP2. Origin must be configured as https.
	HTTP2Origin bool `json:"http2Origin"`
	// Sets the HTTP Host header on requests sent to the local service.
	HTTPHostHeader string `json:"httpHostHeader"`
	// Maximum number of idle keepalive connections between Tunnel and your origin.
	// This does not restrict the total number of concurrent connections.
	KeepAliveConnections int64 `json:"keepAliveConnections"`
	// Timeout after which an idle keepalive connection can be discarded.
	KeepAliveTimeout int64 `json:"keepAliveTimeout"`
	// Disable the “happy eyeballs” algorithm for IPv4/IPv6 fallback if your local
	// network has misconfigured one of the protocols.
	NoHappyEyeballs bool `json:"noHappyEyeballs"`
	// Disables TLS verification of the certificate presented by your origin. Will
	// allow any certificate from the origin to be accepted.
	NoTLSVerify bool `json:"noTLSVerify"`
	// Hostname that cloudflared should expect from your origin server certificate.
	OriginServerName string `json:"originServerName"`
	// cloudflared starts a proxy server to translate HTTP traffic into TCP when
	// proxying, for example, SSH or RDP. This configures what type of proxy will be
	// started. Valid options are: "" for the regular proxy and "socks" for a SOCKS5
	// proxy.
	ProxyType string `json:"proxyType"`
	// The timeout after which a TCP keepalive packet is sent on a connection between
	// Tunnel and the origin server.
	TCPKeepAlive int64 `json:"tcpKeepAlive"`
	// Timeout for completing a TLS handshake to your origin server, if you have chosen
	// to connect Tunnel to an HTTPS server.
	TLSTimeout int64 `json:"tlsTimeout"`
}

func (t *TunnelConfigurationOriginRequest) ToRequestFormat() *zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigOriginRequest {
	value := zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigOriginRequest{}
	isZeroValue := true
	if access := t.Access.ToRequestFormat(); access != nil {
		value.Access = cloudflare.F(*access)
		isZeroValue = false
	}
	if t.CAPool != "" {
		value.CAPool = cloudflare.F(t.CAPool)
		isZeroValue = false
	}
	if t.ConnectTimeout != 0 {
		value.ConnectTimeout = cloudflare.F(t.ConnectTimeout)
		isZeroValue = false
	}
	if t.DisableChunkedEncoding {
		value.DisableChunkedEncoding = cloudflare.F(t.DisableChunkedEncoding)
		isZeroValue = false
	}
	if t.HTTP2Origin {
		value.HTTP2Origin = cloudflare.F(t.HTTP2Origin)
		isZeroValue = false
	}
	if t.HTTPHostHeader != "" {
		value.HTTPHostHeader = cloudflare.F(t.HTTPHostHeader)
		isZeroValue = false
	}
	if t.KeepAliveConnections != 0 {
		value.KeepAliveConnections = cloudflare.F(t.KeepAliveConnections)
		isZeroValue = false
	}
	if t.KeepAliveTimeout != 0 {
		value.KeepAliveTimeout = cloudflare.F(t.KeepAliveTimeout)
		isZeroValue = false
	}
	if t.NoHappyEyeballs {
		value.NoHappyEyeballs = cloudflare.F(t.NoHappyEyeballs)
		isZeroValue = false
	}
	if t.NoTLSVerify {
		value.NoTLSVerify = cloudflare.F(t.NoTLSVerify)
		isZeroValue = false
	}
	if t.OriginServerName != "" {
		value.OriginServerName = cloudflare.F(t.OriginServerName)
		isZeroValue = false
	}
	if t.ProxyType != "" {
		value.ProxyType = cloudflare.F(t.ProxyType)
		isZeroValue = false
	}
	if t.TCPKeepAlive != 0 {
		value.TCPKeepAlive = cloudflare.F(t.TCPKeepAlive)
		isZeroValue = false
	}
	if t.TLSTimeout != 0 {
		value.TLSTimeout = cloudflare.F(t.TLSTimeout)
		isZeroValue = false
	}
	if isZeroValue {
		return nil
	}
	return &value
}

// For all L7 requests to this hostname, cloudflared will validate each request's
// Cf-Access-Jwt-Assertion request header.
type TunnelConfigurationOriginRequestAccess struct {
	// Access applications that are allowed to reach this hostname for this Tunnel.
	// Audience tags can be identified in the dashboard or via the List Access policies
	// API.
	AUDTag   []string `json:"audTag"`
	TeamName string   `json:"teamName"`
	// Deny traffic that has not fulfilled Access authorization.
	Required bool `json:"required"`
}

func (t *TunnelConfigurationOriginRequestAccess) ToRequestFormat() *zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigOriginRequestAccess {
	value := zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigOriginRequestAccess{}
	isZeroValue := true
	if len(t.AUDTag) > 0 {
		value.AUDTag = cloudflare.F(t.AUDTag)
		isZeroValue = false
	}
	if t.TeamName != "" {
		value.TeamName = cloudflare.F(t.TeamName)
		isZeroValue = false
	}
	if t.Required {
		value.Required = cloudflare.F(t.Required)
		isZeroValue = false
	}
	if isZeroValue {
		return nil
	}
	return &value
}

type TunnelConfigurationIngressOriginRequest struct {
	Access                 TunnelConfigurationIngressOriginRequestAccess `json:"access"`
	CAPool                 string                                        `json:"caPool"`
	ConnectTimeout         int64                                         `json:"connectTimeout"`
	DisableChunkedEncoding bool                                          `json:"disableChunkedEncoding"`
	HTTP2Origin            bool                                          `json:"http2Origin"`
	HTTPHostHeader         string                                        `json:"httpHostHeader"`
	KeepAliveConnections   int64                                         `json:"keepAliveConnections"`
	KeepAliveTimeout       int64                                         `json:"keepAliveTimeout"`
	NoHappyEyeballs        bool                                          `json:"noHappyEyeballs"`
	NoTLSVerify            bool                                          `json:"noTLSVerify"`
	OriginServerName       string                                        `json:"originServerName"`
	ProxyType              string                                        `json:"proxyType"`
	TCPKeepAlive           int64                                         `json:"tcpKeepAlive"`
	TLSTimeout             int64                                         `json:"tlsTimeout"`
}

func (t *TunnelConfigurationIngressOriginRequest) ToRequestFormat() *zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngressOriginRequest {
	value := zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngressOriginRequest{}
	isZeroValue := true
	if access := t.Access.ToRequestFormat(); access != nil {
		value.Access = cloudflare.F(*access)
		isZeroValue = false
	}
	if t.CAPool != "" {
		value.CAPool = cloudflare.F(t.CAPool)
		isZeroValue = false
	}
	if t.ConnectTimeout != 0 {
		value.ConnectTimeout = cloudflare.F(t.ConnectTimeout)
		isZeroValue = false
	}
	if t.DisableChunkedEncoding {
		value.DisableChunkedEncoding = cloudflare.F(t.DisableChunkedEncoding)
		isZeroValue = false
	}
	if t.HTTP2Origin {
		value.HTTP2Origin = cloudflare.F(t.HTTP2Origin)
		isZeroValue = false
	}
	if t.HTTPHostHeader != "" {
		value.HTTPHostHeader = cloudflare.F(t.HTTPHostHeader)
		isZeroValue = false
	}
	if t.KeepAliveConnections != 0 {
		value.KeepAliveConnections = cloudflare.F(t.KeepAliveConnections)
		isZeroValue = false
	}
	if t.KeepAliveTimeout != 0 {
		value.KeepAliveTimeout = cloudflare.F(t.KeepAliveTimeout)
		isZeroValue = false
	}
	if t.NoHappyEyeballs {
		value.NoHappyEyeballs = cloudflare.F(t.NoHappyEyeballs)
		isZeroValue = false
	}
	if t.NoTLSVerify {
		value.NoTLSVerify = cloudflare.F(t.NoTLSVerify)
		isZeroValue = false
	}
	if t.OriginServerName != "" {
		value.OriginServerName = cloudflare.F(t.OriginServerName)
		isZeroValue = false
	}
	if t.ProxyType != "" {
		value.ProxyType = cloudflare.F(t.ProxyType)
		isZeroValue = false
	}
	if t.TCPKeepAlive != 0 {
		value.TCPKeepAlive = cloudflare.F(t.TCPKeepAlive)
		isZeroValue = false
	}
	if t.TLSTimeout != 0 {
		value.TLSTimeout = cloudflare.F(t.TLSTimeout)
		isZeroValue = false
	}
	if isZeroValue {
		return nil
	}
	return &value
}

type TunnelConfigurationIngressOriginRequestAccess struct {
	AUDTag   []string `json:"audTag"`
	TeamName string   `json:"teamName"`
	Required bool     `json:"required"`
}

func (t *TunnelConfigurationIngressOriginRequestAccess) ToRequestFormat() *zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngressOriginRequestAccess {
	value := zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngressOriginRequestAccess{}
	isZeroValue := true
	if len(t.AUDTag) > 0 {
		value.AUDTag = cloudflare.F(t.AUDTag)
		isZeroValue = false
	}
	if t.TeamName != "" {
		value.TeamName = cloudflare.F(t.TeamName)
		isZeroValue = false
	}
	if t.Required {
		value.Required = cloudflare.F(t.Required)
		isZeroValue = false
	}
	if isZeroValue {
		return nil
	}
	return &value
}

type GetTunnelConfigurationParams struct {
	TunnelID string
}

func (t *TunnelClient) GetTunnelConfiguration(ctx context.Context, params GetTunnelConfigurationParams) (*TunnelConfiguration, error) {
	res, err := t.client.ZeroTrust.Tunnels.Cloudflared.Configurations.Get(
		ctx,
		params.TunnelID,
		zero_trust.TunnelCloudflaredConfigurationGetParams{
			AccountID: cloudflare.F(t.accountID),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("getting current tunnel configuration: %w", err)
	}

	var config TunnelConfiguration
	if err := yamlGoYaml.Unmarshal([]byte(res.Config.JSON.RawJSON()), &config); err != nil {
		return nil, fmt.Errorf("unmarshalling tunnel configuration: %w", err)
	}
	return &config, nil
}

type UpdateIngressConfigParams struct {
	TunnelID string
	// Add or update ingress
	Ingress []zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress
	// Delete ingress by hostname
	DeleteHosts []string
}

// UpdateIngressConfig updates the ingress configuration of the tunnel.
//
// If the last ingress rule doesn't match all URLs, a new ingress rule with `http_status:404` will be appended.
func (t *TunnelClient) UpdateIngressConfig(ctx context.Context, params UpdateIngressConfigParams) error {
	// Get current currConfig
	tConfig, err := t.GetTunnelConfiguration(ctx, GetTunnelConfigurationParams{TunnelID: params.TunnelID})
	if err != nil {
		return fmt.Errorf("getting current tunnel configuration: %w", err)
	}

	reqConfigParams := tConfig.ToRequestFormat()
	if reqConfigParams == nil {
		reqConfigParams = &zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfig{}
	}

	ingressMap := make(map[string]zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress)
	for _, ing := range reqConfigParams.Ingress.Value {
		ingressMap[ing.Hostname.Value] = ing
	}

	// remove existing ingress to be deleted
	for _, host := range params.DeleteHosts {
		delete(ingressMap, host)
	}
	// remove existing ingress to be updated
	for _, ing := range params.Ingress {
		delete(ingressMap, ing.Hostname.Value)
	}

	// add ingress to be added or updated
	ingress := make(
		[]zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress,
		0,
		len(ingressMap)+len(params.Ingress)+1, // +1 for the last ingress rule to match all URLs
	)
	ingress = append(ingress, params.Ingress...)
	for _, ing := range ingressMap {
		ingress = append(ingress, ing)
	}

	// sort by hostname (A->Z) and empty string at the end
	sort.Slice(ingress, func(i, j int) bool {
		if ingress[i].Hostname.Value == "" {
			return false
		}
		if ingress[j].Hostname.Value == "" {
			return true
		}
		return ingress[i].Hostname.Value < ingress[j].Hostname.Value
	})

	// The last ingress rule must match all URLs.
	// Otherwise, automatically append a new ingress rule with `http_status:404` to match all URLs
	// to avoid the following error:
	//
	// ```json
	// // 400 Bad Request
	// {
	//   "success":false,
	//   "errors":[
	//     {
	//       "code":1056,
	//       "message":"Bad Configuration: Validation failed: The last ingress
	//                  rule must match all URLs (i.e. it should not have a hostname or path filter)\n"}
	//   ],
	//   "messages":[],
	//   "result":null
	// }
	// ```
	if len(ingress) == 0 || ingress[len(ingress)-1].Hostname.Value != "" || ingress[len(ingress)-1].Path.Value != "" {
		ingress = append(ingress, zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress{
			Service: cloudflare.F("http_status:404"),
		})
	}

	reqConfigParams.Ingress = cloudflare.F(ingress)
	jsonBytes, err := yamlGoYaml.Marshal(reqConfigParams)
	if err != nil {
		return fmt.Errorf("marshalling request configuration parameters: %w", err)
	}
	fmt.Println("req config params: ", string(jsonBytes))
	if _, err := t.client.ZeroTrust.Tunnels.Cloudflared.Configurations.Update(
		ctx,
		params.TunnelID,
		zero_trust.TunnelCloudflaredConfigurationUpdateParams{
			AccountID: cloudflare.F(t.accountID),
			Config:    cloudflare.F(*reqConfigParams),
		},
	); err != nil {
		return fmt.Errorf("updating tunnel configuration: %w", err)
	}
	return nil
}
