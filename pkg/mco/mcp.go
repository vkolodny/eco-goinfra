package mco

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/openshift-kni/eco-goinfra/pkg/clients"
	"github.com/openshift-kni/eco-goinfra/pkg/msg"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	fiveScds          time.Duration = 5 * time.Second
	isTrue                          = "True"
	machineConfigPool               = "MachineConfigPool"
)

// MCPBuilder provides struct for MachineConfigPool object which contains connection to cluster
// and MachineConfigPool definitions.
type MCPBuilder struct {
	// MachineConfigPool definition. Used to create MachineConfigPool object with minimum set of required elements.
	Definition *mcov1.MachineConfigPool
	// Created MachineConfigPool object on the cluster.
	Object *mcov1.MachineConfigPool
	// api client to interact with the cluster.
	apiClient *clients.Settings
	// errorMsg is processed before MachineConfigPool object is created.
	errorMsg string
}

// MCPAdditionalOptions additional options for mcp object.
type MCPAdditionalOptions func(builder *MCPBuilder) (*MCPBuilder, error)

// NewMCPBuilder method creates new instance of builder.
func NewMCPBuilder(apiClient *clients.Settings, mcpName string) *MCPBuilder {
	glog.V(100).Infof(
		"Initializing new MCPBuilder structure with the following params: %s", mcpName)

	builder := &MCPBuilder{
		apiClient: apiClient,
		Definition: &mcov1.MachineConfigPool{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcpName,
			},
		},
	}

	if mcpName == "" {
		glog.V(100).Infof("The name of the MachineConfigPool is empty")

		builder.errorMsg = "MachineConfigPool 'name' cannot be empty"
	}

	return builder
}

// Pull pulls existing machineconfigpool from cluster.
func Pull(apiClient *clients.Settings, name string) (*MCPBuilder, error) {
	glog.V(100).Infof("Pulling existing machineconfigpool name %s from cluster", name)

	builder := MCPBuilder{
		apiClient: apiClient,
		Definition: &mcov1.MachineConfigPool{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		},
	}

	if name == "" {
		glog.V(100).Infof("The name of the machineconfigpool is empty")

		builder.errorMsg = "machineconfigpool 'name' cannot be empty"
	}

	if !builder.Exists() {
		return nil, fmt.Errorf("machineconfigpool object %s doesn't exist", name)
	}

	builder.Definition = builder.Object

	return &builder, nil
}

// Create makes a MachineConfigPool in cluster and stores the created object in struct.
func (builder *MCPBuilder) Create() (*MCPBuilder, error) {
	if valid, err := builder.validate(); !valid {
		return builder, err
	}

	glog.V(100).Infof("Creating the MachineConfigPool %s",
		builder.Definition.Name)

	var err error
	if !builder.Exists() {
		builder.Object, err = builder.apiClient.MachineConfigPools().Create(
			context.TODO(), builder.Definition, metav1.CreateOptions{})
	}

	return builder, err
}

// Delete removes a MachineConfigPool object from a cluster.
func (builder *MCPBuilder) Delete() error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("Deleting the MachineConfigPool object %s",
		builder.Definition.Name)

	if !builder.Exists() {
		return fmt.Errorf("MachineConfigPool cannot be deleted because it does not exist")
	}

	err := builder.apiClient.MachineConfigPools().Delete(
		context.TODO(), builder.Object.Name, metav1.DeleteOptions{})

	if err != nil {
		return fmt.Errorf("cannot delete MachineConfigPool: %w", err)
	}

	return err
}

// Exists checks whether the given MachineConfigPool exists.
func (builder *MCPBuilder) Exists() bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof("Checking if the MachineConfigPool object %s exists",
		builder.Definition.Name)

	var err error
	builder.Object, err = builder.apiClient.MachineConfigPools().Get(
		context.Background(), builder.Definition.Name, metav1.GetOptions{})

	return err == nil || !k8serrors.IsNotFound(err)
}

// WithMcSelector defines the machineConfigSelector in the machine config pool.
func (builder *MCPBuilder) WithMcSelector(mcSelector map[string]string) *MCPBuilder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("WithMcSelector updates builder object with "+
		"machineConfigSelector label: %v", mcSelector)

	if len(mcSelector) == 0 {
		builder.errorMsg = "'machineConfigSelector MatchLabels' field cannot be empty"
	}

	if builder.errorMsg != "" {
		return builder
	}

	builder.Definition.Spec.MachineConfigSelector.MatchLabels = mcSelector

	return builder
}

// WaitToBeInCondition waits for a specific time duration until the MachineConfigPool will have a
// specified condition type with the expected status.
func (builder *MCPBuilder) WaitToBeInCondition(
	conditionType mcov1.MachineConfigPoolConditionType,
	conditionStatus corev1.ConditionStatus,
	timeout time.Duration,
) error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("WaitToBeInCondition waits up to specified time duration %v until "+
		"MachineConfigPool condition %v is met", timeout, conditionType)

	return wait.PollImmediate(fiveScds, timeout, func() (bool, error) {
		mcp, err := builder.apiClient.MachineConfigPools().Get(context.Background(),
			builder.Object.Name, metav1.GetOptions{})

		if err != nil {
			return false, nil
		}

		for _, condition := range mcp.Status.Conditions {
			if condition.Type == conditionType && condition.Status == conditionStatus {
				return true, nil
			}
		}

		return false, nil
	})
}

// WaitForUpdate waits for a MachineConfigPool to be updating and then updated.
func (builder *MCPBuilder) WaitForUpdate(timeout time.Duration) error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("WaitForUpdate waits up to specified time %v until updating"+
		" machineConfigPool object is updated", timeout)

	mcpUpdating, err := builder.apiClient.MachineConfigPools().Get(context.Background(),
		builder.Object.Name, metav1.GetOptions{})

	if err != nil {
		return err
	}

	for _, condition := range mcpUpdating.Status.Conditions {
		if condition.Type == "Updating" && condition.Status == isTrue {
			err := wait.PollImmediate(fiveScds, timeout, func() (bool, error) {
				mcpUpdated, err := builder.apiClient.MachineConfigPools().Get(context.Background(),
					builder.Object.Name, metav1.GetOptions{})

				if err != nil {
					return false, nil
				}

				for _, condition := range mcpUpdated.Status.Conditions {
					if condition.Type == "Updated" && condition.Status == isTrue {
						return true, nil
					}
				}

				return false, nil
			})

			if err != nil {
				return err
			}
		}
	}

	return nil
}

// WaitToBeStableFor waits on MachineConfigPool to stable for a time duration or until timeout.
func (builder *MCPBuilder) WaitToBeStableFor(stableDuration time.Duration, timeout time.Duration) error {
	if valid, err := builder.validate(); !valid {
		return err
	}

	glog.V(100).Infof("WaitToBeStableFor waits up to duration of %v for "+
		"MachineConfigPool to be stable for %v", timeout, stableDuration)

	isMcpStable := true

	// Wait 5 secs in each iteration before condition function () returns true or errors
	// or times out after stableDuration
	err := wait.PollImmediate(fiveScds, timeout, func() (bool, error) {

		isMcpStable = true

		_ = wait.PollImmediate(fiveScds, stableDuration, func() (done bool, err error) {

			if !builder.Exists() {
				return false, nil
			}

			if builder.Object.Status.ReadyMachineCount != builder.Object.Status.MachineCount ||
				builder.Object.Status.MachineCount != builder.Object.Status.UpdatedMachineCount ||
				builder.Object.Status.DegradedMachineCount != 0 {

				glog.V(100).Infof("MachineConfigPool: %v degraded and has a mismatch in "+
					"machineCount: %v "+"vs machineCountUpdated: "+"%v vs readyMachineCount: %v and "+
					"degradedMachineCount is : %v \n", builder.Object.ObjectMeta.Name,
					builder.Object.Status.MachineCount, builder.Object.Status.UpdatedMachineCount,
					builder.Object.Status.ReadyMachineCount, builder.Object.Status.DegradedMachineCount)

				isMcpStable = false

				return true, nil
			}

			return false, nil
		})

		if isMcpStable {
			glog.V(100).Infof("MachineConfigPool was stable during during stableDuration: %v",
				stableDuration)

			// this will exit the outer wait.PollImmediate block since the mcp was stable during stableDuration
			return true, nil
		}

		glog.V(100).Infof("MachineConfigPool was not stable during stableDuration: %v, retrying ...",
			stableDuration)

		// keep iterating in the outer wait.PollImmediate waiting for cluster to be stable
		return false, nil
	})

	// After the timout in outer wait.PollImmediate.
	if err == nil {
		glog.V(100).Infof("Cluster was stable during stableDuration: %v", stableDuration)
	} else {
		// Here err is "timed out waiting for the condition"
		glog.V(100).Infof("Cluster was Un-stable during stableDuration: %v", stableDuration)
	}

	return err
}

// WithOptions creates mcp with generic mutation options.
func (builder *MCPBuilder) WithOptions(options ...MCPAdditionalOptions) *MCPBuilder {
	if valid, _ := builder.validate(); !valid {
		return builder
	}

	glog.V(100).Infof("Setting mcp additional options")

	for _, option := range options {
		if option != nil {
			builder, err := option(builder)

			if err != nil {
				glog.V(100).Infof("Error occurred in mutation function")

				builder.errorMsg = err.Error()

				return builder
			}
		}
	}

	return builder
}

// IsInCondition parses MachineConfigPool conditions.
// Returns true if given MachineConfigPool is in given condition, otherwise false.
func (builder *MCPBuilder) IsInCondition(mcpConditionType mcov1.MachineConfigPoolConditionType) bool {
	if valid, _ := builder.validate(); !valid {
		return false
	}

	glog.V(100).Infof("IsInCondition returns true"+
		" if MachineConfigPool object is in a given condition %v, otherwise false", mcpConditionType)

	if builder.Exists() {
		for _, condition := range builder.Object.Status.Conditions {
			if condition.Type == mcpConditionType && condition.Status == isTrue {
				return true
			}
		}
	}

	return false
}

// validate will check that the builder and builder definition are properly initialized before
// accessing any member fields.
func (builder *MCPBuilder) validate() (bool, error) {
	resourceCRD := "MachineConfigPool"

	if builder == nil {
		glog.V(100).Infof("The %s builder is uninitialized", resourceCRD)

		return false, fmt.Errorf("error: received nil %s builder", resourceCRD)
	}

	if builder.Definition == nil {
		glog.V(100).Infof("The %s is undefined", resourceCRD)

		builder.errorMsg = msg.UndefinedCrdObjectErrString(resourceCRD)
	}

	if builder.apiClient == nil {
		glog.V(100).Infof("The %s builder apiclient is nil", resourceCRD)

		builder.errorMsg = fmt.Sprintf("%s builder cannot have nil apiClient", resourceCRD)
	}

	if builder.errorMsg != "" {
		glog.V(100).Infof("The %s builder has error message: %s", resourceCRD, builder.errorMsg)

		return false, fmt.Errorf(builder.errorMsg)
	}

	return true, nil
}
