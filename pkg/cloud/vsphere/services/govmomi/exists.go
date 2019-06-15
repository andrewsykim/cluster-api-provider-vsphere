package govmomi

import (
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/cloud/vsphere/context"
)

// Exists returns a flag indicating whether or not a machine exists.
func Exists(ctx *context.MachineContext) (bool, error) {
	if ctx.MachineConfig.MachineRef == "" {
		ctx.Logger.V(4).Info("exists is false due to lack of machine ref")
		return false, nil
	}

	moRef := types.ManagedObjectReference{
		Type:  "VirtualMachine",
		Value: ctx.MachineConfig.MachineRef,
	}

	var obj mo.VirtualMachine
	if err := ctx.Session.RetrieveOne(ctx, moRef, []string{"name"}, &obj); err != nil {
		ctx.Logger.V(4).Info("exists is false due to lookup failure")
		return false, nil
	}

	return true, nil
}
