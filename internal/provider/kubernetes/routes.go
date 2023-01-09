// Copyright Envoy Gateway Authors
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package kubernetes

import (
	"context"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwapiv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/envoyproxy/gateway/internal/gatewayapi"
	"github.com/envoyproxy/gateway/internal/provider/utils"
)

// processTLSRoutes finds TLSRoutes corresponding to a gatewayNamespaceName, further checks for
// the backend references and pushes the TLSRoutes to the resourceTree.
func (r *gatewayAPIReconciler) processTLSRoutes(ctx context.Context, gatewayNamespaceName string,
	resourceMap *resourceMappings, resourceTree *gatewayapi.Resources) error {
	tlsRouteList := &gwapiv1a2.TLSRouteList{}
	if err := r.client.List(ctx, tlsRouteList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(gatewayTLSRouteIndex, gatewayNamespaceName),
	}); err != nil {
		r.log.Error(err, "unable to find associated TLSRoutes")
		return err
	}

	for _, tlsRoute := range tlsRouteList.Items {
		tlsRoute := tlsRoute
		r.log.Info("processing TLSRoute", "namespace", tlsRoute.Namespace, "name", tlsRoute.Name)

		for _, rule := range tlsRoute.Spec.Rules {
			for _, backendRef := range rule.BackendRefs {
				backendRef := backendRef
				ref := gatewayapi.UpgradeBackendRef(backendRef)
				if err := validateBackendRef(&ref); err != nil {
					r.log.Error(err, "invalid backendRef")
					continue
				}

				backendNamespace := gatewayapi.NamespaceDerefOrAlpha(backendRef.Namespace, tlsRoute.Namespace)
				resourceMap.allAssociatedBackendRefs[types.NamespacedName{
					Namespace: backendNamespace,
					Name:      string(backendRef.Name),
				}] = struct{}{}

				if backendNamespace != tlsRoute.Namespace {
					from := ObjectKindNamespacedName{kind: gatewayapi.KindTLSRoute, namespace: tlsRoute.Namespace, name: tlsRoute.Name}
					to := ObjectKindNamespacedName{kind: gatewayapi.KindService, namespace: backendNamespace, name: string(backendRef.Name)}
					refGrant, err := r.findReferenceGrant(ctx, from, to)
					switch {
					case err != nil:
						r.log.Error(err, "failed to find ReferenceGrant")
					case refGrant == nil:
						r.log.Info("no matching ReferenceGrants found", "from", from.kind,
							"from namespace", from.namespace, "target", to.kind, "target namespace", to.namespace)
					default:
						resourceMap.allAssociatedRefGrants[utils.NamespacedName(refGrant)] = refGrant
						r.log.Info("added ReferenceGrant to resource map", "namespace", refGrant.Namespace,
							"name", refGrant.Name)
					}
				}
			}
		}

		resourceMap.allAssociatedNamespaces[tlsRoute.Namespace] = struct{}{}
		resourceTree.TLSRoutes = append(resourceTree.TLSRoutes, &tlsRoute)
	}

	return nil
}

// processGRPCRoutes finds GRPCRoutes corresponding to a gatewayNamespaceName, further checks for
// the backend references and pushes the GRPCRoutes to the resourceTree.
func (r *gatewayAPIReconciler) processGRPCRoutes(ctx context.Context, gatewayNamespaceName string,
	resourceMap *resourceMappings, resourceTree *gatewayapi.Resources) error {
	grpcRouteList := &gwapiv1a2.GRPCRouteList{}
	if err := r.client.List(ctx, grpcRouteList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(gatewayGRPCRouteIndex, gatewayNamespaceName),
	}); err != nil {
		r.log.Error(err, "failed to list GRPCRoutes")
		return err
	}
	for _, grpcRoute := range grpcRouteList.Items {
		grpcRoute := grpcRoute
		r.log.Info("processing GRPCRoute", "namespace", grpcRoute.Namespace, "name", grpcRoute.Name)

		for _, rule := range grpcRoute.Spec.Rules {
			for _, backendRef := range rule.BackendRefs {
				backendRef := backendRef
				if err := validateBackendRef(&backendRef.BackendRef); err != nil {
					r.log.Error(err, "invalid backendRef")
					continue
				}

				backendNamespace := gatewayapi.NamespaceDerefOr(backendRef.Namespace, grpcRoute.Namespace)
				resourceMap.allAssociatedBackendRefs[types.NamespacedName{
					Namespace: backendNamespace,
					Name:      string(backendRef.Name),
				}] = struct{}{}

				if backendNamespace != grpcRoute.Namespace {
					from := ObjectKindNamespacedName{
						kind:      gatewayapi.KindGRPCRoute,
						namespace: grpcRoute.Namespace,
						name:      grpcRoute.Name,
					}
					to := ObjectKindNamespacedName{
						kind:      gatewayapi.KindService,
						namespace: backendNamespace,
						name:      string(backendRef.Name),
					}
					refGrant, err := r.findReferenceGrant(ctx, from, to)
					switch {
					case err != nil:
						r.log.Error(err, "failed to find ReferenceGrant")
					case refGrant == nil:
						r.log.Info("no matching ReferenceGrants found", "from", from.kind,
							"from namespace", from.namespace, "target", to.kind, "target namespace", to.namespace)
					default:
						resourceMap.allAssociatedRefGrants[utils.NamespacedName(refGrant)] = refGrant
						r.log.Info("added ReferenceGrant to resource map", "namespace", refGrant.Namespace,
							"name", refGrant.Name)
					}
				}
			}

			for i := range rule.Filters {
				filter := rule.Filters[i]
				if err := gatewayapi.ValidateGRPCRouteFilter(&filter); err != nil {
					r.log.Error(err, "bypassing filter rule", "index", i)
					continue
				}
			}
		}

		resourceMap.allAssociatedNamespaces[grpcRoute.Namespace] = struct{}{}
		resourceTree.GRPCRoutes = append(resourceTree.GRPCRoutes, &grpcRoute)
	}

	return nil
}

// processHTTPRoutes finds HTTPRoutes corresponding to a gatewayNamespaceName, further checks for
// the backend references and pushes the HTTPRoutes to the resourceTree.
func (r *gatewayAPIReconciler) processHTTPRoutes(ctx context.Context, gatewayNamespaceName string,
	resourceMap *resourceMappings, resourceTree *gatewayapi.Resources) error {
	httpRouteList := &gwapiv1b1.HTTPRouteList{}
	if err := r.client.List(ctx, httpRouteList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(gatewayHTTPRouteIndex, gatewayNamespaceName),
	}); err != nil {
		r.log.Error(err, "failed to list HTTPRoutes")
		return err
	}
	for _, httpRoute := range httpRouteList.Items {
		httpRoute := httpRoute
		r.log.Info("processing HTTPRoute", "namespace", httpRoute.Namespace, "name", httpRoute.Name)

		var authenFilters []*egv1a1.AuthenticationFilter
		for _, rule := range httpRoute.Spec.Rules {
			for _, backendRef := range rule.BackendRefs {
				backendRef := backendRef
				if err := validateBackendRef(&backendRef.BackendRef); err != nil {
					r.log.Error(err, "invalid backendRef")
					continue
				}

				backendNamespace := gatewayapi.NamespaceDerefOr(backendRef.Namespace, httpRoute.Namespace)
				resourceMap.allAssociatedBackendRefs[types.NamespacedName{
					Namespace: backendNamespace,
					Name:      string(backendRef.Name),
				}] = struct{}{}

				if backendNamespace != httpRoute.Namespace {
					from := ObjectKindNamespacedName{
						kind:      gatewayapi.KindHTTPRoute,
						namespace: httpRoute.Namespace,
						name:      httpRoute.Name,
					}
					to := ObjectKindNamespacedName{
						kind:      gatewayapi.KindService,
						namespace: backendNamespace,
						name:      string(backendRef.Name),
					}
					refGrant, err := r.findReferenceGrant(ctx, from, to)
					switch {
					case err != nil:
						r.log.Error(err, "failed to find ReferenceGrant")
					case refGrant == nil:
						r.log.Info("no matching ReferenceGrants found", "from", from.kind,
							"from namespace", from.namespace, "target", to.kind, "target namespace", to.namespace)
					default:
						resourceMap.allAssociatedRefGrants[utils.NamespacedName(refGrant)] = refGrant
						r.log.Info("added ReferenceGrant to resource map", "namespace", refGrant.Namespace,
							"name", refGrant.Name)
					}
				}
			}

			for i := range rule.Filters {
				filter := rule.Filters[i]
				if err := gatewayapi.ValidateHTTPRouteFilter(&filter); err != nil {
					r.log.Error(err, "bypassing filter rule", "index", i)
					continue
				}

				if filter.Type == gwapiv1b1.HTTPRouteFilterExtensionRef {
					authenFilter, err := r.getAuthenticationFilter(ctx, httpRoute.Namespace, string(filter.ExtensionRef.Name))
					if err != nil {
						r.log.Error(err, "bypassing filter rule", "index", i)
						continue
					}

					authenFilters = append(authenFilters, authenFilter)
				}
			}
		}

		if len(authenFilters) > 0 {
			resourceMap.httpRouteToAuthenFilters[utils.NamespacedName(&httpRoute)] = authenFilters
		}

		resourceMap.allAssociatedNamespaces[httpRoute.Namespace] = struct{}{}
		resourceTree.HTTPRoutes = append(resourceTree.HTTPRoutes, &httpRoute)
	}

	return nil
}

// processTCPRoutes finds TCPRoutes corresponding to a gatewayNamespaceName, further checks for
// the backend references and pushes the TCPRoutes to the resourceTree.
func (r *gatewayAPIReconciler) processTCPRoutes(ctx context.Context, gatewayNamespaceName string,
	resourceMap *resourceMappings, resourceTree *gatewayapi.Resources) error {
	tcpRouteList := &gwapiv1a2.TCPRouteList{}
	if err := r.client.List(ctx, tcpRouteList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(gatewayTCPRouteIndex, gatewayNamespaceName),
	}); err != nil {
		r.log.Error(err, "unable to find associated UDPRoutes")
		return err
	}

	for _, tcpRoute := range tcpRouteList.Items {
		tcpRoute := tcpRoute
		r.log.Info("processing TCPRoute", "namespace", tcpRoute.Namespace, "name", tcpRoute.Name)

		for _, rule := range tcpRoute.Spec.Rules {
			for _, backendRef := range rule.BackendRefs {
				backendRef := backendRef
				ref := gatewayapi.UpgradeBackendRef(backendRef)
				if err := validateBackendRef(&ref); err != nil {
					r.log.Error(err, "invalid backendRef")
					continue
				}

				backendNamespace := gatewayapi.NamespaceDerefOrAlpha(backendRef.Namespace, tcpRoute.Namespace)
				resourceMap.allAssociatedBackendRefs[types.NamespacedName{
					Namespace: backendNamespace,
					Name:      string(backendRef.Name),
				}] = struct{}{}

				if backendNamespace != tcpRoute.Namespace {
					from := ObjectKindNamespacedName{kind: gatewayapi.KindTCPRoute, namespace: tcpRoute.Namespace, name: tcpRoute.Name}
					to := ObjectKindNamespacedName{kind: gatewayapi.KindService, namespace: backendNamespace, name: string(backendRef.Name)}
					refGrant, err := r.findReferenceGrant(ctx, from, to)
					switch {
					case err != nil:
						r.log.Error(err, "failed to find ReferenceGrant")
					case refGrant == nil:
						r.log.Info("no matching ReferenceGrants found", "from", from.kind,
							"from namespace", from.namespace, "target", to.kind, "target namespace", to.namespace)
					default:
						resourceMap.allAssociatedRefGrants[utils.NamespacedName(refGrant)] = refGrant
						r.log.Info("added ReferenceGrant to resource map", "namespace", refGrant.Namespace,
							"name", refGrant.Name)
					}
				}
			}
		}

		resourceMap.allAssociatedNamespaces[tcpRoute.Namespace] = struct{}{}
		resourceTree.TCPRoutes = append(resourceTree.TCPRoutes, &tcpRoute)
	}

	return nil
}

// processUDPRoutes finds UDPRoutes corresponding to a gatewayNamespaceName, further checks for
// the backend references and pushes the UDPRoutes to the resourceTree.
func (r *gatewayAPIReconciler) processUDPRoutes(ctx context.Context, gatewayNamespaceName string,
	resourceMap *resourceMappings, resourceTree *gatewayapi.Resources) error {
	udpRouteList := &gwapiv1a2.UDPRouteList{}
	if err := r.client.List(ctx, udpRouteList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(gatewayUDPRouteIndex, gatewayNamespaceName),
	}); err != nil {
		r.log.Error(err, "unable to find associated UDPRoutes")
		return err
	}

	for _, udpRoute := range udpRouteList.Items {
		udpRoute := udpRoute
		r.log.Info("processing UDPRoute", "namespace", udpRoute.Namespace, "name", udpRoute.Name)

		for _, rule := range udpRoute.Spec.Rules {
			for _, backendRef := range rule.BackendRefs {
				backendRef := backendRef
				ref := gatewayapi.UpgradeBackendRef(backendRef)
				if err := validateBackendRef(&ref); err != nil {
					r.log.Error(err, "invalid backendRef")
					continue
				}

				backendNamespace := gatewayapi.NamespaceDerefOrAlpha(backendRef.Namespace, udpRoute.Namespace)
				resourceMap.allAssociatedBackendRefs[types.NamespacedName{
					Namespace: backendNamespace,
					Name:      string(backendRef.Name),
				}] = struct{}{}

				if backendNamespace != udpRoute.Namespace {
					from := ObjectKindNamespacedName{kind: gatewayapi.KindUDPRoute, namespace: udpRoute.Namespace, name: udpRoute.Name}
					to := ObjectKindNamespacedName{kind: gatewayapi.KindService, namespace: backendNamespace, name: string(backendRef.Name)}
					refGrant, err := r.findReferenceGrant(ctx, from, to)
					switch {
					case err != nil:
						r.log.Error(err, "failed to find ReferenceGrant")
					case refGrant == nil:
						r.log.Info("no matching ReferenceGrants found", "from", from.kind,
							"from namespace", from.namespace, "target", to.kind, "target namespace", to.namespace)
					default:
						resourceMap.allAssociatedRefGrants[utils.NamespacedName(refGrant)] = refGrant
						r.log.Info("added ReferenceGrant to resource map", "namespace", refGrant.Namespace,
							"name", refGrant.Name)
					}
				}
			}
		}

		resourceMap.allAssociatedNamespaces[udpRoute.Namespace] = struct{}{}
		resourceTree.UDPRoutes = append(resourceTree.UDPRoutes, &udpRoute)
	}

	return nil
}