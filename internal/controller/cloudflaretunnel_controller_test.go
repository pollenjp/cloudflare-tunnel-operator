/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cloudflaretunnelv1alpha1 "github.com/pollenjp/cloudflare-tunnel-operator/api/v1alpha1"
	"github.com/pollenjp/cloudflare-tunnel-operator/pkg/cf"
)

// MockTunnelClient is a mock implementation of TunnelClientInterface for testing
type MockTunnelClient struct {
	tunnels      map[string]*cf.Tunnel
	tunnelTokens map[string]string
}

func NewMockTunnelClient() *MockTunnelClient {
	return &MockTunnelClient{
		tunnels:      make(map[string]*cf.Tunnel),
		tunnelTokens: make(map[string]string),
	}
}

func (m *MockTunnelClient) NewTunnel(ctx context.Context, params cf.TunnelNewParams) (*cf.Tunnel, error) {
	// Check if tunnel with the same name already exists
	for _, tunnel := range m.tunnels {
		if tunnel.Name == params.Name {
			return nil, errors.New("tunnel already exists with name: " + params.Name)
		}
	}

	tunnelID := "tunnel-" + params.Name + "-id"
	tunnel := &cf.Tunnel{
		ID:   tunnelID,
		Name: params.Name,
	}
	m.tunnels[tunnelID] = tunnel
	m.tunnelTokens[tunnelID] = "token-" + tunnelID
	return tunnel, nil
}

func (m *MockTunnelClient) FindTunnel(ctx context.Context, params cf.FindTunnelParams) (*cf.Tunnel, error) {
	for _, tunnel := range m.tunnels {
		if tunnel.Name == params.Name {
			return tunnel, nil
		}
	}
	return nil, cf.ErrFindTunnelNotFound
}

func (m *MockTunnelClient) DeleteTunnel(ctx context.Context, params cf.DeleteTunnelParams) error {
	delete(m.tunnels, params.TunnelID)
	delete(m.tunnelTokens, params.TunnelID)
	return nil
}

func (m *MockTunnelClient) GetTunnelToken(ctx context.Context, params cf.GetTunnelTokenParams) (*string, error) {
	token, ok := m.tunnelTokens[params.TunnelID]
	if !ok {
		return nil, errors.New("tunnel not found: " + params.TunnelID)
	}
	return &token, nil
}

var _ = Describe("CloudflareTunnel Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		cloudflaretunnel := &cloudflaretunnelv1alpha1.CloudflareTunnel{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind CloudflareTunnel")
			err := k8sClient.Get(ctx, typeNamespacedName, cloudflaretunnel)
			if err != nil && apierrors.IsNotFound(err) {
				resource := &cloudflaretunnelv1alpha1.CloudflareTunnel{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					// TODO(user): Specify other spec details if needed.
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &cloudflaretunnelv1alpha1.CloudflareTunnel{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance CloudflareTunnel")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			mockTunnelClient := NewMockTunnelClient()
			controllerReconciler := &CloudflareTunnelReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				TunnelClient: mockTunnelClient,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify that the tunnel was created
			updatedResource := &cloudflaretunnelv1alpha1.CloudflareTunnel{}
			err = k8sClient.Get(ctx, typeNamespacedName, updatedResource)
			Expect(err).NotTo(HaveOccurred())

			// Verify that the tunnel status was set
			Expect(updatedResource.Status.Tunnel).NotTo(BeNil())
			Expect(updatedResource.Status.Tunnel.ID).To(Equal("tunnel-" + resourceName + "-id"))
			Expect(updatedResource.Status.Tunnel.Name).To(Equal(resourceName))
		})
	})
})
