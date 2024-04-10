package migrate

import (
	"context"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/claim"

	"github.com/crossplane-contrib/claim-migrator/resource"

	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	errCreateDestClaim        = "cannot create destination claim "
	errGetResource            = "cannot get requested resource"
	errKubeConfig             = "failed to get kubeconfig"
	errGetMapping             = "cannot get mapping for resource"
	errMissingName            = "missing name, must be provided separately 'TYPE[.VERSION][.GROUP] [NAME]' or in the 'TYPE[.VERSION][.GROUP][/NAME]' format"
	errNameDoubled            = "name provided twice, must be provided separately 'TYPE[.VERSION][.GROUP] [NAME]' or in the 'TYPE[.VERSION][.GROUP][/NAME]' format"
	errNotNamespaced          = " resource not namespaces"
	errInvalidResource        = "invalid resource, must be provided in the 'TYPE[.VERSION][.GROUP][/NAME]' format"
	errInvalidResourceAndName = "invalid resource and name"
	errRestMapper             = "unable to create REST Mapper"
	errTargetNamespace        = "target namespace does not exist "
)

type Cmd struct {
	Claim         string `arg:"" help:"Kind of the Crossplane Claim, accepts the 'TYPE[.VERSION][.GROUP][/NAME]' format."`
	Namespace     string `short:"n" name:"namespace" help:"Namespace of the existing Claim." default:"default"`
	DestNamespace string `help:"Destination namespace for the Claim."`
	Name          string `arg:"" optional:"" help:"(Optional) Name of the Crossplane Claim, can be passed as part of the <claim> claim.example.com/name."`
}

// Migrate Claim Procedure
// https://github.com/crossplane/crossplane/discussions/4081
//
// - Get a copy of the claim you want to move to another namespace.
// - Change the metadata.namespace to the new one and apply it.
// - Update the references in the corresponding composite:
//   -  metadata.labels[crossplane.io/claim-namespace]
//   -  spec.claimRef.namespace
// Delete the original claim.
// Edit the original claim and remove the spec.resourceRef field. (or you could remove the finalizers)

func (c *Cmd) Run(logger logging.Logger) error {
	ctx := context.Background()
	logger = logger.WithValues("Resource", c.Claim, "Name", c.Name, "SrcNamespace", c.Namespace, "DestNamespace", c.DestNamespace)

	kubeconfig, err := ctrl.GetConfig()
	if err != nil {
		return errors.Wrap(err, errKubeConfig)
	}
	logger.Debug("✅ kubeconfig loaded")

	// Client for dealing with CRDs
	dynamicClient, err := resource.NewDynamicClient(kubeconfig)
	if err != nil {
		return err
	}
	logger.Debug("✅ kubernetes client created")

	// check if destination Namespace exists
	ns := &v1.ObjectReference{
		Kind:       "Namespace",
		APIVersion: "v1",
		Name:       c.DestNamespace,
	}

	_, re, err := resource.GetResource(ctx, dynamicClient, ns)
	if err != nil {
		return errors.Wrap(err, errGetResource)
	}
	if !re {
		return errors.Errorf("❌ cannot create new claim, namespace %s does not exist", c.DestNamespace)
	}
	logger.Info("✅ destination namespace exists")

	rmapper, err := resource.NewRestMapper(kubeconfig)
	if err != nil {
		return errors.Wrap(err, errRestMapper)
	}

	res, name, err := c.getResourceAndName()
	if err != nil {
		return errors.Wrap(err, errInvalidResourceAndName)
	}

	mapping, err := resource.MappingFor(rmapper, res)
	if err != nil {
		return errors.Wrap(err, errGetMapping)
	}
	if mapping.Scope.Name() != meta.RESTScopeNameNamespace {
		return errors.Wrap(err, errNotNamespaced)
	}

	srcClaimRef := &v1.ObjectReference{
		Kind:       mapping.GroupVersionKind.Kind,
		APIVersion: mapping.GroupVersionKind.GroupVersion().String(),
		Name:       name,
		Namespace:  c.Namespace,
	}

	srcClaim, re, err := resource.GetResource(ctx, dynamicClient, srcClaimRef)
	if err != nil {
		return errors.Wrap(err, errGetResource)
	}
	if !re {
		logger.Info("❌ source Claim not found")
		return errors.Wrap(err, "source Claim not found")
	}
	logger.Info("✅ source Claim exists")

	dstClaimRef := &v1.ObjectReference{
		Kind:       mapping.GroupVersionKind.Kind,
		APIVersion: mapping.GroupVersionKind.GroupVersion().String(),
		Name:       name,
		Namespace:  c.DestNamespace,
	}

	// check if destination Claim exists
	_, re, err = resource.GetResource(ctx, dynamicClient, dstClaimRef)
	if err != nil {
		return errors.Wrap(err, errGetResource)
	}
	if re {
		return errors.Errorf("Cannot create new claim: claim %s in namespace %s already exists ", name, c.DestNamespace)
	}

	// create the destination Claim
	dstClaim := srcClaim.DeepCopy()
	dstClaim.SetNamespace(c.DestNamespace)
	_ = fieldpath.Pave(dstClaim.Object).SetValue("metadata.resourceVersion", "")

	dstClaimUnstructured, err := resource.CreateResource(ctx, dynamicClient, dstClaimRef, dstClaim)
	if err != nil {
		return errors.Wrap(err, errCreateDestClaim)
	}

	// Get the XR associated with the claim
	xrc := claim.New(
		claim.WithGroupVersionKind(srcClaimRef.GroupVersionKind()),
	)
	xrc.Unstructured = *dstClaimUnstructured.DeepCopy()
	xr := xrc.GetResourceReference()

	// Update the Composite
	err = resource.UpdateCompositeWithNewClaim(ctx, dynamicClient, xr, xrc)
	if err != nil {
		return errors.Wrap(err, "unable to update composite")
	}
	logger.Info("✅ XR updated with new Claim", "name", xr.Name)
	logger.Info("✅ Migration complete")

	// Delete the Source Claim
	err = resource.DeleteSourceClaim(ctx, dynamicClient, srcClaimRef)
	if err != nil {
		return errors.Wrap(err, "unable to delete source claim")
	}
	logger.Info("✅ source Claim deleted", "name", srcClaimRef.Name)

	return nil
}

func (c *Cmd) getResourceAndName() (string, string, error) {
	// If no resource was provided, error out (should never happen as it's
	// required by Kong)
	if c.Claim == "" {
		return "", "", errors.New(errInvalidResource)
	}

	// Split the resource into its components
	splittedResource := strings.Split(c.Claim, "/")
	length := len(splittedResource)

	if length == 1 {
		// If no name is provided, error out
		if c.Name == "" {
			return "", "", errors.New(errMissingName)
		}

		// Resource has only kind and the name is separately provided
		return splittedResource[0], c.Name, nil
	}

	if length == 2 {
		// If a name is separately provided, error out
		if c.Name != "" {
			return "", "", errors.New(errNameDoubled)
		}

		// Resource includes both kind and name
		return splittedResource[0], splittedResource[1], nil
	}

	// Handle the case when resource format is invalid
	return "", "", errors.New(errInvalidResource)
}
