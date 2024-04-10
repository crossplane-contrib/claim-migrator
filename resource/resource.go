package resource

import (
	"context"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/claim"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/composite"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"
)

// Get Resource gets a resource. Returns false
func GetResource(ctx context.Context, client dynamic.Interface, ref *v1.ObjectReference) (*unstructured.Unstructured, bool, error) {
	res := client.Resource(schema.GroupVersionResource{
		Group:    ref.GroupVersionKind().Group,
		Version:  ref.GroupVersionKind().Version,
		Resource: strings.ToLower(ref.Kind) + "s",
	}).Namespace(ref.Namespace)

	u, err := res.Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err

	}
	return u, true, nil
}

// Check if Resource Exists
func ResourceExists(ctx context.Context, client dynamic.Interface, ref *v1.ObjectReference) (bool, error) {
	_, re, err := GetResource(ctx, client, ref)
	return re, err
}

// CreateResource creates a K8s resource using a dynamic Client, allowing us to create CRD types
func CreateResource(ctx context.Context, client dynamic.Interface, ref *v1.ObjectReference, u *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	res := client.Resource(schema.GroupVersionResource{
		Group:    ref.GroupVersionKind().Group,
		Version:  ref.GroupVersionKind().Version,
		Resource: strings.ToLower(ref.Kind) + "s",
	}).Namespace(ref.Namespace)

	u, err := res.Create(ctx, u, metav1.CreateOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "unable to create new claim")
	}

	return u, nil
}

// UpdateCompositeWithNewClaim updates the Composite to refer to the new Claim
func UpdateCompositeWithNewClaim(ctx context.Context, client dynamic.Interface, xrRef *v1.ObjectReference, xrcu *claim.Unstructured) error {
	res := schema.GroupVersionResource{
		Group:    xrRef.GroupVersionKind().Group,
		Version:  xrRef.GroupVersionKind().Version,
		Resource: strings.ToLower(xrRef.Kind) + "s",
	}

	// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		xru, getErr := client.Resource(res).Get(ctx, xrRef.Name, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		// Update values
		xr := composite.New()
		xr.SetGroupVersionKind(xrRef.GroupVersionKind())
		xr.Unstructured = *xru.DeepCopy()
		xr.SetClaimReference(xrcu.GetReference())

		labels := xr.GetLabels()
		labels["crossplane.io/claim-namespace"] = xrcu.GetNamespace()
		xr.SetLabels(labels)

		_, updateErr := client.Resource(res).Update(context.TODO(), &xr.Unstructured, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		return retryErr
	}
	return nil
}

// DeleteSourceClaim removes references to the Composite before deleting
func DeleteSourceClaim(ctx context.Context, client dynamic.Interface, ref *v1.ObjectReference) error {
	res := client.Resource(schema.GroupVersionResource{
		Group:    ref.GroupVersionKind().Group,
		Version:  ref.GroupVersionKind().Version,
		Resource: strings.ToLower(ref.Kind) + "s",
	}).Namespace(ref.Namespace)

	// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
	retryUpdateErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		u, getErr := res.Get(ctx, ref.Name, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		// Update Claim to remove Composite Reference and finalizers
		xrc := claim.New()
		xrc.SetGroupVersionKind(ref.GroupVersionKind())
		xrc.Unstructured = *u.DeepCopy()
		xrc.SetResourceReference(nil)
		xrc.SetFinalizers([]string{})

		_, updateErr := res.Update(ctx, &xrc.Unstructured, metav1.UpdateOptions{})
		return updateErr

	})
	if retryUpdateErr != nil {
		return retryUpdateErr
	}

	retryDeleteErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Get the newest version of the Claim
		_, getErr := res.Get(ctx, ref.Name, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		deleteErr := res.Delete(ctx, ref.Name, metav1.DeleteOptions{})
		return deleteErr
	})
	if retryDeleteErr != nil {
		return retryDeleteErr
	}

	return nil
}
