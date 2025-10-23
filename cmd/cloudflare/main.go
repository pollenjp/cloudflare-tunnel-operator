// Sample program to manage zero trust access policies and applications

package main

import (
	"context"
	"errors"

	// "encoding/base64"
	"log"
	"os"

	// "github.com/cloudflare/cloudflare-go/v6"
	// "github.com/cloudflare/cloudflare-go/v6/zero_trust"
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

	// {
	// 	if err := tunnelClient.UpdateIngressConfig(ctx, cf.UpdateIngressConfigParams{
	// 		TunnelID: tunnel.ID,
	// 		Ingress: []zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress{
	// 			{
	// 				Hostname: cloudflare.F("sample3.pollenjp.com"),
	// 				Service:  cloudflare.F("http://localhost:8080"),
	// 			},
	// 		},
	// 	}); err != nil {
	// 		log.Fatal(err)
	// 	}
	// 	log.Println("succeeded to update ingress configuration")
	// }

	// tunnelToken, err := client.ZeroTrust.Tunnels.Cloudflared.Token.Get(
	// 	ctx,
	// 	zeroTrustTunnelId,
	// 	zero_trust.TunnelCloudflaredTokenGetParams{
	// 		AccountID: cloudflare.F(ACCOUNT_ID),
	// 	},
	// )
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// log.Println("succeeded to get tunnel token")
	// // `{"a": 'xxxx', "t": 'yyyy', "s": 'zzzz'}` json string encoded in base64
	// // a: account tag
	// // t: tunnel id
	// // s: secret (base64 encoded)
	// log.Println("tunnel token: ", *tunnelToken)

	// // policy

	// policyName := "sample-zero-trust-policy"
	// policyId := ""

	// // check existing policy

	// policyList, err := client.ZeroTrust.Access.Policies.List(
	// 	ctx,
	// 	zero_trust.AccessPolicyListParams{
	// 		AccountID: cloudflare.F(ACCOUNT_ID),
	// 	},
	// )
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// log.Println("succeeded to list policies")

	// for _, policy := range policyList.Result {
	// 	log.Println("policy: ", policy.ID, "name: ", policy.Name)

	// 	if policy.Name == policyName {
	// 		log.Println("policy (", policy.Name, ") already exists", "decision: ", policy.Decision)
	// 		policyId = policy.ID
	// 		// TODO: check the policy differences. If there are any differences, update the policy.
	// 	}
	// }

	// // create new policy

	// if policyId == "" {
	// 	newPolicy, err := client.ZeroTrust.Access.Policies.New(
	// 		ctx,
	// 		zero_trust.AccessPolicyNewParams{
	// 			AccountID: cloudflare.F(ACCOUNT_ID),
	// 			Name:      cloudflare.F(policyName),
	// 			Decision:  cloudflare.F(zero_trust.DecisionAllow),
	// 			Include: cloudflare.F([]zero_trust.AccessRuleUnionParam{
	// 				zero_trust.EmailRuleParam{
	// 					Email: cloudflare.F(zero_trust.EmailRuleEmailParam{
	// 						Email: cloudflare.F(MY_EMAIL),
	// 					}),
	// 				},
	// 			}),
	// 		},
	// 	)
	// 	if err != nil {
	// 		log.Fatal(err)
	// 	}
	// 	policyId = newPolicy.ID
	// 	log.Println("succeeded to create policy")
	// 	log.Println("policy: ", newPolicy.ID, "name: ", newPolicy.Name, "decision: ", newPolicy.Decision)
	// }

	// // zero trust access application

	// domain := "sample." + MY_BASE_DOMAIN // FIXME: later
	// zeroTrustAppName := "sample-zero-trust-app"
	// zeroTrustAppId := ""

	// appList, err := client.ZeroTrust.Access.Applications.List(
	// 	ctx,
	// 	zero_trust.AccessApplicationListParams{
	// 		AccountID: cloudflare.F(ACCOUNT_ID),
	// 		Name:      cloudflare.F(zeroTrustAppName),
	// 	},
	// )
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// switch len(appList.Result) {
	// case 0:
	// 	log.Println("no application with the same name found")
	// 	// create a new app
	// case 1:
	// 	log.Println("application: ", appList.Result[0].ID, "name: ", appList.Result[0].Name)
	// 	zeroTrustAppId = appList.Result[0].ID
	// 	// FIXME: check the app differences. If there are any differences, update the app.
	// default:
	// 	log.Fatal("multiple applications with the same name found")
	// }
	// log.Println("succeeded to list applications")

	// if zeroTrustAppId == "" {
	// 	newApp, err := client.ZeroTrust.Access.Applications.New(
	// 		ctx,
	// 		zero_trust.AccessApplicationNewParams{
	// 			AccountID: cloudflare.F(ACCOUNT_ID),
	// 			Body: zero_trust.AccessApplicationNewParamsBodySelfHostedApplication{
	// 				Name:                    cloudflare.F(zeroTrustAppName),
	// 				Domain:                  cloudflare.F(domain),
	// 				HTTPOnlyCookieAttribute: cloudflare.F(true),
	// 				Type:                    cloudflare.F(zero_trust.ApplicationTypeSelfHosted),
	// 				Destinations: cloudflare.F([]zero_trust.AccessApplicationNewParamsBodySelfHostedApplicationDestinationUnion{
	// 					zero_trust.AccessApplicationNewParamsBodySelfHostedApplicationDestinationsPublicDestination{
	// 						Type: cloudflare.F(zero_trust.AccessApplicationNewParamsBodySelfHostedApplicationDestinationsPublicDestinationTypePublic),
	// 						URI:  cloudflare.F(domain),
	// 					},
	// 				}),
	// 				Policies: cloudflare.F([]zero_trust.AccessApplicationNewParamsBodySelfHostedApplicationPolicyUnion{
	// 					zero_trust.AccessApplicationNewParamsBodySelfHostedApplicationPoliciesAccessAppPolicyLink{
	// 						ID:         cloudflare.F(policyId),
	// 						Precedence: cloudflare.F(int64(20)),
	// 					},
	// 				}),
	// 			},
	// 		},
	// 	)
	// 	if err != nil {
	// 		log.Fatal(err)
	// 	}
	// 	zeroTrustAppId = newApp.ID
	// 	log.Println("succeeded to create application")
	// 	log.Println("application: ", newApp.ID, "name: ", newApp.Name)
	// }

	// log.Println("succeeded to create zero trust application")
	// log.Println("application: ", zeroTrustAppId, "name: ", zeroTrustAppName)

}
