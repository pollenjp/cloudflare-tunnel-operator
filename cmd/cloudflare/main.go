// Sample program to manage zero trust access policies and applications

package main

import (
	"context"
	"encoding/base64"
	"log"
	"os"

	"github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/option"
	"github.com/cloudflare/cloudflare-go/v6/zero_trust"
)

const (
	ACCOUNT_ID = "8a9c08698370c234cf710f9f19b8ded1"
	ZONE_ID = "359ac5178328172d440d922194e4c643"
	MY_BASE_DOMAIN = "pollenjp.com"
	MY_EMAIL = "polleninjp@gmail.com"
)

func main() {
	ctx := context.Background()

	log.Println("start")

	opts := []option.RequestOption{}

	if o, ok := os.LookupEnv("CLOUDFLARE_API_TOKEN"); ok {
		opts = append(opts, option.WithAPIToken(o))
	} else {
		log.Fatal("CLOUDFLARE_API_TOKEN is not set")
	}

	client := cloudflare.NewClient(opts...)
	log.Println("succeeded to create client")

	// cloudflared
	// TODO: create a cloudflared tunnel

	zeroTrustTunnelName := "sample-zero-trust-tunnel"
	zeroTrustTunnelId := ""

	page, err := client.ZeroTrust.Tunnels.Cloudflared.List(
		ctx,
		zero_trust.TunnelCloudflaredListParams{
			AccountID: cloudflare.F(ACCOUNT_ID),
			Name: cloudflare.F(zeroTrustTunnelName),
		},
	)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("succeeded to get tunnels")
	for _, tunnel := range page.Result {
		isHealthy := tunnel.Status == "healthy"
		log.Println("tunnel: ", tunnel.ID, "name: ", tunnel.Name, "status: ", tunnel.Status, "healthy: ", isHealthy)
		if tunnel.Name == zeroTrustTunnelName {
			zeroTrustTunnelId = tunnel.ID
			log.Println("zero trust tunnel already exists")
			continue
		}
		// if !isHealthy {
		// 	// delete
		// 	delRes, err := client.ZeroTrust.Tunnels.Cloudflared.Delete(
		// 		ctx,
		// 		tunnel.ID,
		// 		zero_trust.TunnelCloudflaredDeleteParams{
		// 			AccountID: cloudflare.F(ACCOUNT_ID),
		// 		},
		// 	)
		// 	if err != nil {
		// 		log.Fatal(err)
		// 	}
		// 	log.Println("succeeded to delete unhealthy tunnel, deleted at: ", delRes.DeletedAt)
		// }
	}

	if zeroTrustTunnelId == "" {
		tunnelSecret, ok := os.LookupEnv("CLOUDFLARE_ZERO_TRUST_TUNNEL_SECRET")
		if !ok {
			log.Fatal("CLOUDFLARE_ZERO_TRUST_TUNNEL_SECRET is not set")
		}

		newTunnel, err := client.ZeroTrust.Tunnels.Cloudflared.New(
			ctx,
			zero_trust.TunnelCloudflaredNewParams{
				AccountID: cloudflare.F(ACCOUNT_ID),
				Name: cloudflare.F(zeroTrustTunnelName),
				ConfigSrc: cloudflare.F(zero_trust.TunnelCloudflaredNewParamsConfigSrcLocal),
				TunnelSecret: cloudflare.F(base64.StdEncoding.EncodeToString([]byte(tunnelSecret))),
			},
		)
		if err != nil {
			log.Fatal(err)
		}
		zeroTrustTunnelId = newTunnel.ID
		log.Println("succeeded to create tunnel")
		log.Println("tunnel: ", newTunnel.ID, "name: ", newTunnel.Name)
	}

	log.Println("Finally, Zero Trust Tunnel is created. tunnel: ", zeroTrustTunnelId, "name: ", zeroTrustTunnelName)

	// get token for the tunnel

	tunnelToken, err := client.ZeroTrust.Tunnels.Cloudflared.Token.Get(
		ctx,
		zeroTrustTunnelId,
		zero_trust.TunnelCloudflaredTokenGetParams{
			AccountID: cloudflare.F(ACCOUNT_ID),
		},
	)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("succeeded to get tunnel token")
	// `{"a": 'xxxx', "t": 'yyyy', "s": 'zzzz'}` json string encoded in base64
	// a: account tag
	// t: tunnel id
	// s: secret (base64 encoded)
	log.Println("tunnel token: ", *tunnelToken)

	// policy

	policyName := "sample-zero-trust-policy"
	policyId := ""

	// check existing policy

	policyList, err := client.ZeroTrust.Access.Policies.List(
		ctx,
		zero_trust.AccessPolicyListParams{
			AccountID: cloudflare.F(ACCOUNT_ID),
		},
	)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("succeeded to list policies")

	for _, policy := range policyList.Result {
		log.Println("policy: ", policy.ID, "name: ", policy.Name)

		if policy.Name == policyName {
			log.Println("policy (", policy.Name, ") already exists", "decision: ", policy.Decision)
			policyId = policy.ID
			// TODO: check the policy differences. If there are any differences, update the policy.
		}
	}

	// create new policy

	if policyId == "" {
		newPolicy, err := client.ZeroTrust.Access.Policies.New(
			ctx,
			zero_trust.AccessPolicyNewParams{
				AccountID: cloudflare.F(ACCOUNT_ID),
				Name: cloudflare.F(policyName),
				Decision: cloudflare.F(zero_trust.DecisionAllow),
				Include: cloudflare.F([]zero_trust.AccessRuleUnionParam{
					zero_trust.EmailRuleParam{
						Email: cloudflare.F(zero_trust.EmailRuleEmailParam{
							Email: cloudflare.F(MY_EMAIL),
						}),
					},
				}),
			},
		)
		if err != nil {
			log.Fatal(err)
		}
		policyId = newPolicy.ID
		log.Println("succeeded to create policy")
		log.Println("policy: ", newPolicy.ID, "name: ", newPolicy.Name, "decision: ", newPolicy.Decision)
	}

	// zero trust access application

	domain := "sample." + MY_BASE_DOMAIN // FIXME: later
	zeroTrustAppName := "sample-zero-trust-app"
	zeroTrustAppId := ""

	appList, err := client.ZeroTrust.Access.Applications.List(
		ctx,
		zero_trust.AccessApplicationListParams{
			AccountID: cloudflare.F(ACCOUNT_ID),
			Name: cloudflare.F(zeroTrustAppName),
		},
	)
	if err != nil {
		log.Fatal(err)
	}
	switch len(appList.Result) {
	case 0:
		log.Println("no application with the same name found")
		// create a new app
	case 1:
		log.Println("application: ", appList.Result[0].ID, "name: ", appList.Result[0].Name)
		zeroTrustAppId = appList.Result[0].ID
		// FIXME: check the app differences. If there are any differences, update the app.
	default:
		log.Fatal("multiple applications with the same name found")
	}
	log.Println("succeeded to list applications")

	if zeroTrustAppId == "" {
		newApp, err := client.ZeroTrust.Access.Applications.New(
			ctx,
			zero_trust.AccessApplicationNewParams{
				AccountID: cloudflare.F(ACCOUNT_ID),
				Body: zero_trust.AccessApplicationNewParamsBodySelfHostedApplication{
					Name: cloudflare.F(zeroTrustAppName),
					Domain: cloudflare.F(domain),
					HTTPOnlyCookieAttribute: cloudflare.F(true),
					Type: cloudflare.F(zero_trust.ApplicationTypeSelfHosted),
					Destinations: cloudflare.F([]zero_trust.AccessApplicationNewParamsBodySelfHostedApplicationDestinationUnion{
						zero_trust.AccessApplicationNewParamsBodySelfHostedApplicationDestinationsPublicDestination{
							Type: cloudflare.F(zero_trust.AccessApplicationNewParamsBodySelfHostedApplicationDestinationsPublicDestinationTypePublic),
							URI: cloudflare.F(domain),
						},
					}),
					Policies: cloudflare.F([]zero_trust.AccessApplicationNewParamsBodySelfHostedApplicationPolicyUnion{
						zero_trust.AccessApplicationNewParamsBodySelfHostedApplicationPoliciesAccessAppPolicyLink{
							ID: cloudflare.F(policyId),
							Precedence: cloudflare.F(int64(20)),
						},
					}),
				},
			},
		)
		if err != nil {
			log.Fatal(err)
		}
		zeroTrustAppId = newApp.ID
		log.Println("succeeded to create application")
		log.Println("application: ", newApp.ID, "name: ", newApp.Name)
	}

	log.Println("succeeded to create zero trust application")
	log.Println("application: ", zeroTrustAppId, "name: ", zeroTrustAppName)

}
