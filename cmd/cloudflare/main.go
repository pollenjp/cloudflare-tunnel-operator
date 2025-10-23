// Sample program to manage zero trust access policies and applications

package main

import (
	"context"
	"errors"

	// "encoding/base64"
	"log"
	"os"

	"github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/zero_trust"
	"github.com/pollenjp/cloudflare-tunnel-operator/pkg/cf"
	yamlGoYaml "go.yaml.in/yaml/v4"
)

var (
	ACCOUNT_ID     string
	ZONE_ID        string
	MY_BASE_DOMAIN string
	MY_EMAIL       string
)

func main() {
	ctx := context.Background()

	ACCOUNT_ID, ok := os.LookupEnv("CLOUDFLARE_ACCOUNT_ID")
	if !ok {
		log.Fatal("CLOUDFLARE_ACCOUNT_ID is not set")
	}
	// ZONE_ID, ok := os.LookupEnv("CLOUDFLARE_ZONE_ID")
	// if !ok {
	// 	log.Fatal("CLOUDFLARE_ZONE_ID is not set")
	// }
	// MY_BASE_DOMAIN, ok := os.LookupEnv("MY_BASE_DOMAIN")
	// if !ok {
	// 	log.Fatal("MY_BASE_DOMAIN is not set")
	// }
	// MY_EMAIL, ok := os.LookupEnv("MY_EMAIL")
	// if !ok {
	// 	log.Fatal("MY_EMAIL is not set")
	// }

	log.Println("start")

	apiToken, ok := os.LookupEnv("CLOUDFLARE_API_TOKEN")
	if !ok {
		log.Fatal("CLOUDFLARE_API_TOKEN is not set")
	}

	zeroTrustTunnelName := "cloudflaretunnel-sample"
	tunnelClient := cf.NewTunnelClient(cf.TunnelClientNewParams{
		AccountID: ACCOUNT_ID,
		APIToken:  apiToken,
		// TunnelNamePrefix: "SAMPLE-",
		TunnelNamePrefix: "CFTO-",
	})
	log.Println("succeeded to create client")

	// cloudflared
	// create a cloudflared tunnel

	log.Println("search existing tunnels ----------------------------------------")
	tunnel, err := tunnelClient.FindTunnel(ctx, cf.FindTunnelParams{
		Name: zeroTrustTunnelName,
	})
	if err != nil && errors.Is(err, cf.ErrFindTunnelNotFound) {
		log.Println("tunnel not found")
		tunnel = nil
	} else if err != nil {
		log.Fatal(err)
	}
	if tunnel != nil {
		log.Println("tunnel: ", tunnel.ID, "name: ", tunnel.Name)
	}

	// if zeroTrustTunnelId != "" {
	// 	log.Println("delete tunnel ----------------------------------------")
	// 	// 'Delete' is idempotent
	// 	deleteRes, err := client.ZeroTrust.Tunnels.Cloudflared.Delete(
	// 		ctx,
	// 		zeroTrustTunnelId,
	// 		zero_trust.TunnelCloudflaredDeleteParams{
	// 			AccountID: cloudflare.F(ACCOUNT_ID),
	// 		},
	// 	)
	// 	if err != nil {
	// 		log.Fatal(err)
	// 	}
	// 	log.Println("succeeded to delete tunnel")
	// 	log.Println("tunnel: ", deleteRes.ID, "name: ", deleteRes.Name, "deletedAt: ", deleteRes.DeletedAt)

	// 	zeroTrustTunnelId = "" // reset
	// }

	if tunnel == nil {
		log.Println("create new tunnel ----------------------------------------")
		// tunnelSecret, ok := os.LookupEnv("CLOUDFLARE_ZERO_TRUST_TUNNEL_SECRET")
		// if !ok {
		// 	log.Fatal("CLOUDFLARE_ZERO_TRUST_TUNNEL_SECRET is not set")
		// }

		newTunnel, err := tunnelClient.NewTunnel(ctx, cf.TunnelNewParams{
			Name: zeroTrustTunnelName,
		})
		if err != nil {
			log.Fatal(err)
		}
		tunnel = newTunnel

		log.Println("Zero Trust Tunnel is newly created. tunnel: ", tunnel.ID, "name: ", tunnel.Name)
	}

	// get token
	{
		tunnelToken, err := tunnelClient.GetTunnelToken(ctx, cf.GetTunnelTokenParams{
			TunnelID: tunnel.ID,
		})
		if err != nil {
			log.Fatal(err)
		}
		log.Println("tunnel token: ", *tunnelToken)
	}

	log.Println("get current tunnel configuration ----------------------------------------")

	{
		tunnelConfig, err := tunnelClient.GetTunnelConfiguration(ctx, cf.GetTunnelConfigurationParams{
			TunnelID: tunnel.ID,
		})
		if err != nil {
			log.Fatal(err)
		}

		// marshal to yaml
		yamlBytes, err := yamlGoYaml.Marshal(tunnelConfig)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("tunnel configuration: ", string(yamlBytes))
	}

	// update

	{
		if err := tunnelClient.UpdateIngressConfig(ctx, cf.UpdateIngressConfigParams{
			TunnelID: tunnel.ID,
			Ingress: []zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress{
				{
					Hostname: cloudflare.F("sample3.pollenjp.com"),
					Service:  cloudflare.F("http://localhost:8080"),
				},
			},
		}); err != nil {
			log.Fatal(err)
		}
		log.Println("succeeded to update ingress configuration")
	}
}
