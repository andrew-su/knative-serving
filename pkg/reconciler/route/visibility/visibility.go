/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package visibility

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apilabels "k8s.io/apimachinery/pkg/labels"
	listers "k8s.io/client-go/listers/core/v1"
	"knative.dev/pkg/network"
	netv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/domains"
	"knative.dev/serving/pkg/reconciler/route/resources/labels"
)

type Config struct {
	serviceLister listers.ServiceLister
}

func NewConfig(l listers.ServiceLister) *Config {
	return &Config{serviceLister: l}
}

func (b *Config) getServices(route *v1alpha1.Route) (map[string]*corev1.Service, error) {
	// List all the Services owned by this Route.
	currentServices, err := b.serviceLister.Services(route.Namespace).List(apilabels.SelectorFromSet(
		apilabels.Set{
			serving.RouteLabelKey: route.Name,
		},
	))
	if err != nil {
		return nil, err
	}

	serviceCopy := make(map[string]*corev1.Service, len(currentServices))
	for _, svc := range currentServices {
		serviceCopy[svc.Name] = svc.DeepCopy()
	}

	return serviceCopy, err
}

func (b *Config) routeVisibility(ctx context.Context, route *v1alpha1.Route) netv1alpha1.IngressVisibility {
	domainConfig := config.FromContext(ctx).Domain
	domain := domainConfig.LookupDomainForLabels(route.Labels)
	if domain == "svc."+network.GetClusterDomainName() {
		return netv1alpha1.IngressVisibilityClusterLocal
	}
	return netv1alpha1.IngressVisibilityExternalIP
}

// ForRoute returns a map of visibility for traffic targets of a given Route.
func (b *Config) ForRoute(ctx context.Context, route *v1alpha1.Route, trafficNames []string) (map[string]netv1alpha1.IngressVisibility, error) {
	// Find out the default visiblity of the Route.
	defaultVisibility := b.routeVisibility(ctx, route)

	// Get all the placeholder Services to check for additional visibility settings.
	services, err := b.getServices(route)
	if err != nil {
		return nil, err
	}
	fmt.Printf("[SERVICES] %#v\n", services)

	m := make(map[string]netv1alpha1.IngressVisibility, len(trafficNames))
	for _, tt := range trafficNames {
		hostname, err := domains.HostnameFromTemplate(ctx, route.Name, tt)
		if err != nil {
			return nil, err
		}
		ttVisibility := netv1alpha1.IngressVisibilityExternalIP
		// Is there a visibility setting on the placeholder Service?
		if svc, ok := services[hostname]; ok {
			if labels.IsObjectLocalVisibility(svc.ObjectMeta) {
				ttVisibility = netv1alpha1.IngressVisibilityClusterLocal
			}
		}

		// Now, choose the lowest visibility.
		m[tt] = minVisibility(ttVisibility, defaultVisibility)
	}
	fmt.Printf("[VISIBILITY] %#v\n", m)
	return m, nil
}

func minVisibility(a, b netv1alpha1.IngressVisibility) netv1alpha1.IngressVisibility {
	if a == netv1alpha1.IngressVisibilityClusterLocal || b == netv1alpha1.IngressVisibilityClusterLocal {
		return netv1alpha1.IngressVisibilityClusterLocal
	}
	return a
}
