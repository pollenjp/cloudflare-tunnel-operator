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
	"fmt"
	"path/filepath"
	"strings"
	"time"

	yamlGoYaml "go.yaml.in/yaml/v4"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	appsv1apply "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	metav1apply "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	cloudflaretunnelv1alpha1 "github.com/pollenjp/cloudflare-tunnel-operator/api/v1alpha1"
	"github.com/pollenjp/cloudflare-tunnel-operator/pkg/cf"
)

const (
	cftunnelFinalizer         = "cloudflare-tunnel.pollenjp.com/finalizer"
	reconcilePeriodicInterval = 5 * time.Minute
	secretTunnelTokenKey      = "token"
	credentialsFilePath       = "/etc/cloudflared/config/credentials.json"
	cloudflaredConfigPath     = "/etc/cloudflared/config/config.yaml"
	cloudflaredMetricsPort    = 2000
	fieldManager              = "cloudflare-tunnel-operator"
)

const (
	typeAvailableCloudflareTunnel = "Available"
	typeDegradedCloudflareTunnel  = "Degraded"
)

var (
	// This error is used to requeue the reconciliation
	ErrReconcileRequeue = errors.New("requeue")
)

// CloudflareTunnelReconciler reconciles a CloudflareTunnel object
type CloudflareTunnelReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	Recorder            record.EventRecorder
	TunnelClient        cf.TunnelClientInterface
	TunnelReclaimPolicy ReclaimPolicy
}

// +kubebuilder:rbac:groups=cloudflare-tunnel.pollenjp.com,resources=cloudflaretunnels,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cloudflare-tunnel.pollenjp.com,resources=cloudflaretunnels/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cloudflare-tunnel.pollenjp.com,resources=cloudflaretunnels/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the CloudflareTunnel object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *CloudflareTunnelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	cftunnel := &cloudflaretunnelv1alpha1.CloudflareTunnel{}
	err := r.Get(ctx, req.NamespacedName, cftunnel)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("CloudflareTunnel resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get CloudflareTunnel")
		return ctrl.Result{}, err
	}

	if len(cftunnel.Status.Conditions) == 0 {
		meta.SetStatusCondition(&cftunnel.Status.Conditions, metav1.Condition{
			Type:               typeAvailableCloudflareTunnel,
			Status:             metav1.ConditionUnknown,
			Reason:             "Reconciling",
			Message:            "Starting reconciliation",
			LastTransitionTime: metav1.Now(),
		})
		if err = r.Status().Update(ctx, cftunnel); err != nil {
			log.Error(err, "Failed to update CloudflareTunnel status")
			return ctrl.Result{}, err
		}

		// Let's re-fetch the CloudflareTunnel Custom Resource after updating the status
		// so that we have the latest state of the resource on the cluster and we will avoid
		// raising the error "the object has been modified, please apply
		// your changes to the latest version and try again" which would re-trigger the reconciliation
		// if we try to update it again in the following operations
		if err := r.Get(ctx, req.NamespacedName, cftunnel); err != nil {
			log.Error(err, "Failed to re-fetch CloudflareTunnel")
			return ctrl.Result{}, err
		}
	}

	// Let's add a finalizer. Then, we can define some operations which should
	// occur before the custom resource is deleted.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers
	if !controllerutil.ContainsFinalizer(cftunnel, cftunnelFinalizer) {
		log.Info("Adding Finalizer for CloudflareTunnel")
		if ok := controllerutil.AddFinalizer(cftunnel, cftunnelFinalizer); !ok {
			err = fmt.Errorf("finalizer for CloudflareTunnel was not added")
			log.Error(err, "Failed to add finalizer for CloudflareTunnel")
			return ctrl.Result{}, err
		}

		if err = r.Update(ctx, cftunnel); err != nil {
			log.Error(err, "Failed to update CloudflareTunnel custom resource to add finalizer")
			return ctrl.Result{}, err
		}

		// Let's re-fetch the CloudflareTunnel Custom Resource after updating
		// so that we have the latest state of the resource on the cluster
		if err := r.Get(ctx, req.NamespacedName, cftunnel); err != nil {
			log.Error(err, "Failed to re-fetch CloudflareTunnel")
			return ctrl.Result{}, err
		}
	}

	// Check if the CloudflareTunnel instance is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	if !cftunnel.GetDeletionTimestamp().IsZero() {
		if controllerutil.ContainsFinalizer(cftunnel, cftunnelFinalizer) {
			log.Info("Performing finalizer operations for CloudflareTunnel before deleting the custom resource")

			// Perform all operations required before removing the finalizer and allow
			// the Kubernetes API to remove the custom resource.
			//
			if err := r.doFinalizerOperationsForCloudflareTunnel(ctx, cftunnel); err != nil {
				log.Error(err, "Failed to perform finalizer operations for CloudflareTunnel")
				return ctrl.Result{}, fmt.Errorf("finalizing the CloudflareTunnel custom resource: %w", err)
			}
			log.Info("Successfully performed finalizer operations for CloudflareTunnel")

			// Re-fetch the Custom Resource before updating the status
			if err := r.Get(ctx, req.NamespacedName, cftunnel); err != nil {
				log.Error(err, "Failed to re-fetch CloudflareTunnel")
				return ctrl.Result{}, err
			}
			meta.SetStatusCondition(
				&cftunnel.Status.Conditions,
				metav1.Condition{
					Type:    typeDegradedCloudflareTunnel,
					Status:  metav1.ConditionTrue,
					Reason:  "Finalizing",
					Message: fmt.Sprintf("Finalizer operations for custom resource %s were successfully accomplished", cftunnel.Name),
				},
			)
			if err := r.Status().Update(ctx, cftunnel); err != nil {
				log.Error(err, "Failed to update CloudflareTunnel status")
				return ctrl.Result{}, err
			}

			log.Info("Removing Finalizer for CloudflareTunnel after successfully perform the operations")

			// Re-fetch the Custom Resource before updating the status
			if err := r.Get(ctx, req.NamespacedName, cftunnel); err != nil {
				log.Error(err, "Failed to re-fetch CloudflareTunnel")
				return ctrl.Result{}, err
			}
			if ok := controllerutil.RemoveFinalizer(cftunnel, cftunnelFinalizer); !ok {
				err = fmt.Errorf("finalizer for CloudflareTunnel was not removed")
				log.Error(err, "Failed to remove finalizer for CloudflareTunnel")
				return ctrl.Result{}, err
			}
			if err := r.Update(ctx, cftunnel); err != nil {
				log.Error(err, "Failed to remove finalizer for CloudflareTunnel")
				return ctrl.Result{}, err
			}

			log.Info("Successfully removed finalizer for CloudflareTunnel")
		}
		return ctrl.Result{}, nil
	}

	// reconcile cloudflare tunnel
	log.Info("reconciling tunnel", "name", cftunnel.Name)
	if err := r.reconcileTunnel(ctx, req, cftunnel); err != nil {
		if errors.Is(err, ErrReconcileRequeue) {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		return ctrl.Result{}, fmt.Errorf("reconciling tunnel: %w", err)
	}

	// reconcile the secret for the tunnel token
	if err := r.reconcileSecretForTunnelToken(ctx, req, cftunnel); err != nil {
		if errors.Is(err, ErrReconcileRequeue) {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		return ctrl.Result{}, fmt.Errorf("reconciling secret for the tunnel token: %w", err)
	}

	// reconcile config map for the cloudflared

	log.Info("reconciling config map for the cloudflared", "name", cftunnel.Name)
	if err := r.reconcileConfigMapForCloudflared(ctx, req, cftunnel); err != nil {
		if errors.Is(err, ErrReconcileRequeue) {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	// reconcile deployment for the cloudflared agent
	if err := r.reconcileDeploymentForCloudflared(ctx, req, cftunnel); err != nil {
		if errors.Is(err, ErrReconcileRequeue) {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: reconcilePeriodicInterval}, nil
}

// Reconcile the tunnel
func (r *CloudflareTunnelReconciler) reconcileTunnel(ctx context.Context, req ctrl.Request, cftunnel *cloudflaretunnelv1alpha1.CloudflareTunnel) error {
	log := logf.FromContext(ctx)

	tunnel, err := r.getExistingTunnel(ctx, cftunnel)
	if err != nil && errors.Is(err, ErrReconcileRequeue) {
		return err
	} else if err != nil && errors.Is(err, ErrGetExistingTunnelNotFound) {
		// create a new tunnel
	} else if err != nil {
		return fmt.Errorf("getting an existing tunnel: %w", err)
	} else {
		// tunnel already exists
		if cftunnel.Status.Tunnel == nil {
			// if tunnel status is not set, set it from the remote tunnel
			log.Info("remote tunnel exists but 'CloudflareTunnel' custom resource 'Status.Tunnel' is not set. updating it from the remote tunnel")
			if err := r.updateTunnelStatus(ctx, req, cftunnel, tunnel); err != nil {
				return fmt.Errorf("updating 'CloudflareTunnel' custom resource 'Status.Tunnel' from the remote tunnel: %w", err)
			}
			// re-fetching the Custom Resource to get the latest state of the resource on the cluster
			if err := r.Get(ctx, req.NamespacedName, cftunnel); err != nil {
				return fmt.Errorf("re-fetching 'CloudflareTunnel' custom resource: %w", err)
			}
		}
		return nil
	}

	// ErrGetExistingTunnelNotFound
	// -> create a new tunnel

	log.Info("creating a new tunnel", "name", cftunnel.Name)
	newTunnel, err := r.TunnelClient.NewTunnel(ctx, cf.TunnelNewParams{
		Name: cftunnel.Name,
	})
	if err != nil {
		return fmt.Errorf("creating a new tunnel: %w", err)
	}

	if err := r.updateTunnelStatus(ctx, req, cftunnel, newTunnel); err != nil {
		return fmt.Errorf("updating 'CloudflareTunnel' custom resource 'Status.Tunnel' from the new tunnel: %w", err)
	}
	return nil
}

var (
	// Q. What for?
	// A. To catch this error and create a new tunnel
	ErrGetExistingTunnelNotFound = errors.New("existing tunnel not found")
)

func (r *CloudflareTunnelReconciler) getExistingTunnel(ctx context.Context, cftunnel *cloudflaretunnelv1alpha1.CloudflareTunnel) (*cf.Tunnel, error) {
	log := logf.FromContext(ctx)

	// check if the remote tunnel exists
	tunnel, err := r.TunnelClient.FindTunnel(ctx, cf.FindTunnelParams{
		Name: cftunnel.Name,
	})
	if err != nil {
		if errors.Is(err, cf.ErrFindTunnelNotFound) {
			log.Info("tunnel does not found", "name", cftunnel.Name)
			return nil, ErrGetExistingTunnelNotFound
		}
		return nil, fmt.Errorf("finding a tunnel: %w", err)
	}
	if cftunnel.Status.Tunnel != nil && cftunnel.Status.Tunnel.ID != tunnel.ID {
		// Same name but different ID -> tunnel has been recreated
		// (old tunnel was deleted and a new tunnel with the same name was created)
		log.Info(
			"same name but different ID. tunnel saving in status will be renewed to the currently active tunnel",
			"oldTunnelID", cftunnel.Status.Tunnel.ID,
			"newTunnelID", tunnel.ID,
		)
		if err := r.deleteTunnelStatus(ctx, cftunnel); err != nil {
			return nil, fmt.Errorf("deleting tunnel status: %w", err)
		}
		// requeue the reconciliation after clearing the tunnel status
		return nil, ErrReconcileRequeue
	}

	// already exists and healthy
	return tunnel, nil
}

// Get the namespaced name for the config map that stores the cloudflared config
func getConfigMapNamespacedNameForCloudflared(cftunnel *cloudflaretunnelv1alpha1.CloudflareTunnel) types.NamespacedName {
	return types.NamespacedName{
		Name:      cftunnel.Name + "-cloudflared-config",
		Namespace: cftunnel.Namespace,
	}
}

// Get the namespaced name for the secret that stores the tunnel token
func getSecretNamespacedNameForTunnelToken(cftunnel *cloudflaretunnelv1alpha1.CloudflareTunnel) types.NamespacedName {
	return types.NamespacedName{
		Name:      cftunnel.Name + "-tunnel-token",
		Namespace: cftunnel.Namespace,
	}
}

// Get the namespaced name for the deployment that runs the cloudflared
func getDeploymentNamespacedNameForCloudflared(cftunnel *cloudflaretunnelv1alpha1.CloudflareTunnel) types.NamespacedName {
	return types.NamespacedName{
		Name:      cftunnel.Name + "-cloudflared",
		Namespace: cftunnel.Namespace,
	}
}

// Reconcile the secret that stores the tunnel token
func (r *CloudflareTunnelReconciler) reconcileSecretForTunnelToken(
	ctx context.Context,
	req ctrl.Request,
	cftunnel *cloudflaretunnelv1alpha1.CloudflareTunnel,
) error {
	log := logf.FromContext(ctx)
	nsName := getSecretNamespacedNameForTunnelToken(cftunnel)

	owner, err := controllerReference(cftunnel, r.Scheme)
	if err != nil {
		return fmt.Errorf("getting owner reference for the secret: %w", err)
	}

	ls := labelsForCloudflareTunnel()
	ls["app.kubernetes.io/instance"] = nsName.Name

	tunnelToken, err := r.TunnelClient.GetTunnelToken(ctx, cf.GetTunnelTokenParams{TunnelID: cftunnel.Status.Tunnel.ID})
	if err != nil {
		return fmt.Errorf("getting a tunnel token from the remote tunnel: %w", err)
	}

	secretApply := corev1apply.Secret(nsName.Name, nsName.Namespace).
		WithLabels(ls).
		WithOwnerReferences(owner).
		WithData(map[string][]byte{
			secretTunnelTokenKey: []byte(*tunnelToken),
		})
		// WithStringData(map[string]string{
		// 	secretTunnelTokenKey: *tunnelToken,
		// })

	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(secretApply)
	if err != nil {
		return fmt.Errorf("converting secret apply configuration to unstructured: %w", err)
	}
	patch := &unstructured.Unstructured{Object: obj}

	var current corev1.Secret
	err = r.Get(ctx, nsName, &current)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	currApplyConfig, err := corev1apply.ExtractSecret(&current, fieldManager)
	if err != nil {
		return fmt.Errorf("extracting current secret apply configuration: %w", err)
	}

	if equality.Semantic.DeepEqual(secretApply, currApplyConfig) {
		log.Info("secret already exists and is up to date", "name", nsName.Name)
		return nil
	}

	if err := r.Patch(ctx, patch, client.Apply, &client.PatchOptions{
		FieldManager: fieldManager,
		// It is strongly recommended for controllers to always force conflicts
		// on objects that they own and manage, since they might not be able to
		// resolve or act on these conflicts.
		// https://kubernetes.io/docs/reference/using-api/server-side-apply/#using-server-side-apply-in-a-controller
		Force: ptr.To(true),
	}); err != nil {
		return fmt.Errorf("patching secret for the tunnel token: %w", err)
	}

	log.Info("reconcile secret successfully")
	return nil
}

func (r *CloudflareTunnelReconciler) reconcileConfigMapForCloudflared(ctx context.Context, req ctrl.Request, cftunnel *cloudflaretunnelv1alpha1.CloudflareTunnel) error {
	log := logf.FromContext(ctx)
	nsName := getConfigMapNamespacedNameForCloudflared(cftunnel)

	owner, err := controllerReference(cftunnel, r.Scheme)
	if err != nil {
		return fmt.Errorf("creating owner reference for the config map: %w", err)
	}

	ls := labelsForCloudflareTunnel()
	ls["app.kubernetes.io/instance"] = nsName.Name

	if cftunnel.Status.Tunnel == nil {
		return fmt.Errorf("tunnel status is not set")
	}

	// TODO: specify from CRD Spec
	cfdConfigYamlBytes, err := (func() ([]byte, error) {
		cfdConfigJson := make(map[string]interface{})
		if cftunnel.Spec.CloudflaredConfig != "" {
			if err := yamlGoYaml.Unmarshal([]byte(cftunnel.Spec.CloudflaredConfig), &cfdConfigJson); err != nil {
				return nil, fmt.Errorf("unmarshalling cloudflared config: %w", err)
			}
		}
		cfdConfigJson["tunnel"] = cftunnel.Status.Tunnel.ID
		if _, ok := cfdConfigJson["no-autoupdate"]; !ok {
			cfdConfigJson["no-autoupdate"] = true
		}
		if _, ok := cfdConfigJson["metrics"]; !ok {
			cfdConfigJson["metrics"] = fmt.Sprintf("0.0.0.0:%d", cloudflaredMetricsPort)
		}

		// type ingressType map[string]interface{}
		// ingressArray, _ := cfdConfigJson["ingress"].([]ingressType)
		// ingressArray = append(ingressArray, ingressType{
		// 	// FIXME: remove later
		// 	"hostname": "sample.pollenjp.com",
		// 	"service":  "sample-service",
		// })
		// cfdConfigJson["ingress"] = ingressArray

		yamlBytes, err := yamlGoYaml.Marshal(cfdConfigJson)
		if err != nil {
			return nil, fmt.Errorf("marshalling cloudflared config: %w", err)
		}
		return yamlBytes, nil
	})()
	if err != nil {
		return fmt.Errorf("embedding config into cloudflared config.yaml: %w", err)
	}

	configMapApply := corev1apply.ConfigMap(nsName.Name, nsName.Namespace).
		WithLabels(ls).
		WithOwnerReferences(owner).
		WithData(map[string]string{
			filepath.Base(cloudflaredConfigPath): string(cfdConfigYamlBytes),
		})

	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(configMapApply)
	if err != nil {
		return err
	}
	patch := &unstructured.Unstructured{
		Object: obj,
	}

	var current corev1.ConfigMap
	err = r.Get(ctx, nsName, &current)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	currApplyConfig, err := corev1apply.ExtractConfigMap(&current, fieldManager)
	if err != nil {
		return err
	}

	if equality.Semantic.DeepEqual(configMapApply, currApplyConfig) {
		return nil
	}

	if err := r.Patch(ctx, patch, client.Apply, &client.PatchOptions{
		FieldManager: fieldManager,
		Force:        ptr.To(true),
	}); err != nil {
		log.Error(err, "unable to create or update config map for the cloudflared")
		return err
	}
	log.Info("reconcile config map for the cloudflared successfully", "name", nsName.Name)
	return nil
}

func (r *CloudflareTunnelReconciler) reconcileDeploymentForCloudflared(ctx context.Context, req ctrl.Request, cftunnel *cloudflaretunnelv1alpha1.CloudflareTunnel) error {
	log := logf.FromContext(ctx)
	nsName := getDeploymentNamespacedNameForCloudflared(cftunnel)

	owner, err := controllerReference(cftunnel, r.Scheme)
	if err != nil {
		return fmt.Errorf("getting owner reference for the deployment: %w", err)
	}

	ls := labelsForCloudflareTunnel()
	ls["app.kubernetes.io/instance"] = nsName.Name

	// https://hub.docker.com/r/cloudflare/cloudflared/tags
	// FIXME: specify in CR Spec
	cloudflaredImage := "mirror.gcr.io/cloudflare/cloudflared:2025.8.1"

	// https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/deployment-guides/kubernetes/
	dep := appsv1apply.Deployment(nsName.Name, nsName.Namespace).
		WithLabels(ls).
		WithOwnerReferences(owner).
		WithSpec(appsv1apply.DeploymentSpec().
			WithReplicas(1). // FIXME: use resource spec
			WithSelector(metav1apply.LabelSelector().WithMatchLabels(ls)).
			WithTemplate(corev1apply.PodTemplateSpec().
				WithLabels(ls).
				WithSpec(corev1apply.PodSpec().
					WithContainers(corev1apply.Container().
						WithName("cloudflared").
						WithImage(cloudflaredImage).
						WithImagePullPolicy(corev1.PullAlways).
						WithEnv(corev1apply.EnvVar().
							WithName("TUNNEL_TOKEN").
							WithValueFrom(corev1apply.EnvVarSource().
								WithSecretKeyRef(corev1apply.SecretKeySelector().
									WithName(getSecretNamespacedNameForTunnelToken(cftunnel).Name).
									WithKey(secretTunnelTokenKey),
								),
							),
						).
						WithArgs(
							"tunnel",
							"--config",
							cloudflaredConfigPath,
							"run",
						).
						WithLivenessProbe(corev1apply.Probe().
							WithHTTPGet(corev1apply.HTTPGetAction().
								WithPath("/ready").
								WithPort(intstr.FromInt(cloudflaredMetricsPort)),
							).
							WithFailureThreshold(20).
							WithInitialDelaySeconds(5).
							WithPeriodSeconds(10),
						).
						WithReadinessProbe(corev1apply.Probe().
							WithHTTPGet(corev1apply.HTTPGetAction().
								WithPath("/ready").
								WithPort(intstr.FromInt(2000)),
							).
							WithSuccessThreshold(10).
							WithInitialDelaySeconds(5).
							WithPeriodSeconds(10),
						).
						WithVolumeMounts(
							corev1apply.VolumeMount().
								WithName("config").
								WithMountPath(filepath.Dir(cloudflaredConfigPath)).
								WithReadOnly(true),
						),
					).
					WithVolumes(
						corev1apply.Volume().
							WithName("config").
							WithConfigMap(corev1apply.ConfigMapVolumeSource().
								WithName(getConfigMapNamespacedNameForCloudflared(cftunnel).Name).
								WithItems(corev1apply.KeyToPath().
									WithKey(filepath.Base(cloudflaredConfigPath)).
									WithPath(filepath.Base(cloudflaredConfigPath)),
								),
							),
					).
					WithAffinity(corev1apply.Affinity().
						WithPodAntiAffinity(corev1apply.PodAntiAffinity().
							WithRequiredDuringSchedulingIgnoredDuringExecution(corev1apply.PodAffinityTerm().
								WithLabelSelector(metav1apply.LabelSelector().WithMatchLabels(ls)).
								WithTopologyKey("kubernetes.io/hostname"),
							),
						),
					).
					WithSecurityContext(corev1apply.PodSecurityContext().
						WithSysctls(corev1apply.Sysctl().
							WithName("net.ipv4.ping_group_range").
							WithValue("65532 65532"),
						),
					),
				),
			),
		)

	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(dep)
	if err != nil {
		return err
	}
	patch := &unstructured.Unstructured{
		Object: obj,
	}

	var current appsv1.Deployment
	err = r.Get(ctx, nsName, &current)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	fieldManager := "cloudflare-tunnel-operator"
	currApplyConfig, err := appsv1apply.ExtractDeployment(&current, fieldManager)
	if err != nil {
		return err
	}

	if equality.Semantic.DeepEqual(dep, currApplyConfig) {
		return nil
	}

	if err := r.Patch(ctx, patch, client.Apply, &client.PatchOptions{
		FieldManager: fieldManager,
		Force:        ptr.To(true),
	}); err != nil {
		log.Error(err, "unable to create or update deployment for the cloudflare agent")
		return err
	}
	log.Info("reconcile deployment for the cloudflare agent successfully", "name", nsName.Name)
	return nil
}

// The labels for selecting the resources
// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func labelsForCloudflareTunnel() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name": "cloudflare-tunnel-operator",
		// "app.kubernetes.io/version":    versionTag,
		"app.kubernetes.io/managed-by": "CloudflareTunnelController",
	}
}

// Update the tunnel status and re-fetch the Custom Resource to get the latest state of the resource on the cluster
func (r *CloudflareTunnelReconciler) updateTunnelStatus(ctx context.Context, req ctrl.Request, cftunnel *cloudflaretunnelv1alpha1.CloudflareTunnel, tunnel *cf.Tunnel) error {
	log := logf.FromContext(ctx)

	cftunnel.Status.Tunnel = &cloudflaretunnelv1alpha1.CloudflareTunnelStatusTunnel{
		ID:   tunnel.ID,
		Name: tunnel.Name,
	}

	// reconcile the secret for the tunnel token
	log.Info("reconciling secret for the tunnel token", "name", cftunnel.Name, "called from", "updateTunnelStatus()")
	if err := r.reconcileSecretForTunnelToken(ctx, req, cftunnel); err != nil {
		return fmt.Errorf("reconciling secret for the tunnel token: %w", err)
	}

	log.Info("updating 'CloudflareTunnel' custom resource 'Status.Tunnel'", "name", cftunnel.Name, "called from", "updateTunnelStatus()")
	if err := r.Status().Update(ctx, cftunnel); err != nil {
		return fmt.Errorf("updating 'CloudflareTunnel' custom resource 'Status.Tunnel': %w", err)
	}
	// re-fetch Custom Resource to get the latest state of the resource on the cluster
	if err := r.Get(ctx, req.NamespacedName, cftunnel); err != nil {
		return fmt.Errorf("re-fetching 'CloudflareTunnel' custom resource: %w", err)
	}
	return nil
}

// Delete Custom Resource Status.Tunnel
//
// If we try to update it again after calling this function,
// re-fetch Custom Resource so that we have the latest state of the resource on the cluster
func (r *CloudflareTunnelReconciler) deleteTunnelStatus(ctx context.Context, cftunnel *cloudflaretunnelv1alpha1.CloudflareTunnel) error {
	log := logf.FromContext(ctx)
	if cftunnel.Status.Tunnel == nil {
		log.Info("tried to delete tunnel status but it has not been set yet. nothing to do")
		return nil
	}

	log.Info("deleting tunnel status")
	cftunnel.Status.Tunnel = nil

	// delete secret for the tunnel token
	secretNamespacedName := getSecretNamespacedNameForTunnelToken(cftunnel)
	if err := r.Delete(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretNamespacedName.Name,
			Namespace: secretNamespacedName.Namespace,
		},
	}); err != nil {
		return fmt.Errorf("deleting secret '%s': %w", secretNamespacedName.Name, err)
	}

	if err := r.Status().Update(ctx, cftunnel); err != nil {
		return err
	}
	log.Info("successfully deleted tunnel status")
	return nil
}

func (r *CloudflareTunnelReconciler) doFinalizerOperationsForCloudflareTunnel(ctx context.Context, cftunnel *cloudflaretunnelv1alpha1.CloudflareTunnel) error {
	log := logf.FromContext(ctx)

	r.Recorder.Event(
		cftunnel, "Warning", "Deleting",
		fmt.Sprintf("Custom Resource %s is being deleted from the namespace %s", cftunnel.Name, cftunnel.Namespace),
	)

	// remove ZeroTrust.Tunnels.Cloudflared
	if r.TunnelReclaimPolicy == ReclaimPolicyDelete && cftunnel.Status.Tunnel != nil {
		log.Info("deleting the remote tunnel", "tunnelID", cftunnel.Status.Tunnel.ID)
		if err := r.TunnelClient.DeleteTunnel(ctx, cf.DeleteTunnelParams{
			TunnelID: cftunnel.Status.Tunnel.ID,
		}); err != nil {
			return fmt.Errorf("deleting the remote tunnel '%s': %w", cftunnel.Status.Tunnel.ID, err)
		}
		log.Info("Successfully deleted the remote tunnel", "tunnelID", cftunnel.Status.Tunnel.ID, "reclaimPolicy", r.TunnelReclaimPolicy)

		// No need to delete the tunnel status since the custom resource is deleted
	}

	// TODO: remove ZeroTrust.Access.Policies

	// TODO: remove ZeroTrust.Access.Applications
	return nil
}

type ReclaimPolicy string

const (
	ReclaimPolicyDelete ReclaimPolicy = "Delete"
	ReclaimPolicyRetain ReclaimPolicy = "Retain"
)

func NewReclaimPolicy(value string) (ReclaimPolicy, error) {
	switch strings.ToLower(value) {
	case "delete":
		return ReclaimPolicyDelete, nil
	case "retain":
		return ReclaimPolicyRetain, nil
	default:
		return "", fmt.Errorf("invalid 'ReclaimPolicy' value: %s", value)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *CloudflareTunnelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cloudflaretunnelv1alpha1.CloudflareTunnel{}).
		Named("cloudflaretunnel").
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

func controllerReference(cftunnel *cloudflaretunnelv1alpha1.CloudflareTunnel, scheme *runtime.Scheme) (*metav1apply.OwnerReferenceApplyConfiguration, error) {
	gvk, err := apiutil.GVKForObject(cftunnel, scheme)
	if err != nil {
		return nil, err
	}
	ref := metav1apply.OwnerReference().
		WithAPIVersion(gvk.GroupVersion().String()).
		WithKind(gvk.Kind).
		WithName(cftunnel.Name).
		WithUID(cftunnel.GetUID()).
		WithBlockOwnerDeletion(true).
		WithController(true)
	return ref, nil
}
