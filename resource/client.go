/*
Copyright 2023 The Crossplane Authors.

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

package resource

import (
	"fmt"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	memory "k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

const (
	errFmtResourceTypeNotFound = "the server doesn't have a resource type %q"
	errGetDiscoveryClient      = "cannot get discovery client"
)

func NewDynamicClient(config *rest.Config) (dynamic.Interface, error) {
	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return dynClient, nil
}

func NewRestMapper(kubeconfig *rest.Config) (meta.RESTMapper, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(kubeconfig)
	if err != nil {
		return nil, errors.Wrap(err, errGetDiscoveryClient)
	}

	d := memory.NewMemCacheClient(discoveryClient)
	rmapper := restmapper.NewShortcutExpander(restmapper.NewDeferredDiscoveryRESTMapper(d), d, nil)

	return rmapper, nil
}

// MappingFor returns the RESTMapping for the given resource or kind argument.
// Copied over from cli-runtime pkg/resource Builder,
// https://github.com/kubernetes/cli-runtime/blob/9a91d944dd43186c52e0162e12b151b0e460354a/pkg/resource/builder.go#L768
func MappingFor(rmapper meta.RESTMapper, resourceOrKindArg string) (*meta.RESTMapping, error) {
	// TODO(phisco): actually use the Builder.
	fullySpecifiedGVR, groupResource := schema.ParseResourceArg(resourceOrKindArg)
	gvk := schema.GroupVersionKind{}
	if fullySpecifiedGVR != nil {
		gvk, _ = rmapper.KindFor(*fullySpecifiedGVR)
	}
	if gvk.Empty() {
		gvk, _ = rmapper.KindFor(groupResource.WithVersion(""))
	}
	if !gvk.Empty() {
		return rmapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	}
	fullySpecifiedGVK, groupKind := schema.ParseKindArg(resourceOrKindArg)
	if fullySpecifiedGVK == nil {
		gvk := groupKind.WithVersion("")
		fullySpecifiedGVK = &gvk
	}
	if !fullySpecifiedGVK.Empty() {
		if mapping, err := rmapper.RESTMapping(fullySpecifiedGVK.GroupKind(), fullySpecifiedGVK.Version); err == nil {
			return mapping, nil
		}
	}
	mapping, err := rmapper.RESTMapping(groupKind, gvk.Version)
	if err != nil {
		// if we error out here, it is because we could not match a resource or a kind
		// for the given argument. To maintain consistency with previous behavior,
		// announce that a resource type could not be found.
		// if the error is _not_ a *meta.NoKindMatchError, then we had trouble doing discovery,
		// so we should return the original error since it may help a user diagnose what is actually wrong
		if meta.IsNoMatchError(err) {
			return nil, fmt.Errorf(errFmtResourceTypeNotFound, groupResource.Resource)
		}
		return nil, err
	}
	return mapping, nil
}
