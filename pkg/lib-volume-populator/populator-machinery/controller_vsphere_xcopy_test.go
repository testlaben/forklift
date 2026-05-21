package populator_machinery

import (
	"testing"

	api "github.com/kubev2v/forklift/pkg/apis/forklift/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Guard regressions: oVirt/OpenStack keep legacy prime-in-pod behavior; only
// VSphereXcopyVolumePopulator omits prime volumes/mounts.
func TestPopulatorPrimeVolumeBehaviorByKind(t *testing.T) {
	tests := []struct {
		kind            string
		referencesPrime bool
		mountsPrime     bool
		isXcopy         bool
	}{
		{api.VSphereXcopyVolumePopulatorKind, false, false, true},
		{api.OvirtVolumePopulatorKind, true, true, false},
		{api.OpenstackVolumePopulatorKind, true, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			c := &controller{gk: schema.GroupKind{Kind: tt.kind}}

			if got := c.populatorPodReferencesPrimeVolume(); got != tt.referencesPrime {
				t.Errorf("populatorPodReferencesPrimeVolume() = %v, want %v", got, tt.referencesPrime)
			}
			if got := c.populatorMountsPrimeVolume(); got != tt.mountsPrime {
				t.Errorf("populatorMountsPrimeVolume() = %v, want %v", got, tt.mountsPrime)
			}
			if got := c.isVSphereXcopyPopulator(); got != tt.isXcopy {
				t.Errorf("isVSphereXcopyPopulator() = %v, want %v", got, tt.isXcopy)
			}
		})
	}
}
